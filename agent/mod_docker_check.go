package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Checking containers for a newer image: the Docker daemon asks the registry for the digest of the tag
// (GET /distribution/{name}/json, with the credentials of the daemon), and the digest is compared with
// the digests the local image was pulled with. The image itself is not downloaded.
//
// The result of the last check is kept in memory and is part of docker.state, so it survives a page
// reload. An entry is dropped once the container runs another image than it did at the check.

type checkEntry struct {
	imageID string // image id of the container at the time of the check
	status  string // "new", "current" or "unknown"
	local   string // short digest on the host
	remote  string // short digest in the registry
	err     string
}

var dockerChecks = struct {
	mu      sync.Mutex
	at      int64
	entries map[string]checkEntry // by container id
}{entries: map[string]checkEntry{}}

// checkFor returns the stored result for a container, if it is still valid.
func checkFor(containerID, imageID string) (checkEntry, bool) {
	dockerChecks.mu.Lock()
	defer dockerChecks.mu.Unlock()
	e, ok := dockerChecks.entries[containerID]
	return e, ok && e.imageID == imageID
}

func checkedAt() int64 {
	dockerChecks.mu.Lock()
	defer dockerChecks.mu.Unlock()
	return dockerChecks.at
}

type dImageInfo struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
}

// shortDigest cuts "repo@sha256:abcdef..." or "sha256:abcdef..." down to 12 hex characters.
func shortDigest(d string) string {
	if i := strings.LastIndex(d, "sha256:"); i >= 0 {
		d = d[i+len("sha256:"):]
	}
	if len(d) > 12 {
		d = d[:12]
	}
	return d
}

// classifyImage decides the state of one image reference: the local image, the digest in the registry.
func classifyImage(containerImageID string, local dImageInfo, remoteDigest string) checkEntry {
	e := checkEntry{imageID: containerImageID}
	if local.ID != containerImageID {
		// the tag was pulled again after the container started: the new image is already on the host
		e.status = "new"
		e.local = shortDigest(containerImageID)
		e.remote = shortDigest(local.ID)
		return e
	}
	if len(local.RepoDigests) == 0 {
		e.status, e.err = "unknown", "the image was not pulled from a registry"
		return e
	}
	for _, d := range local.RepoDigests {
		if i := strings.LastIndex(d, "@"); i >= 0 && d[i+1:] == remoteDigest {
			e.status = "current"
			e.local = shortDigest(remoteDigest)
			e.remote = e.local
			return e
		}
	}
	e.status = "new"
	e.local = shortDigest(local.RepoDigests[0])
	e.remote = shortDigest(remoteDigest)
	return e
}

// dockerCheck is the command "docker.check": compares the images of all containers with the registry.
func dockerCheck(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	if !dockerAvailable() {
		return nil, errors.New("docker is not available")
	}
	var cs []dContainer
	if err := dockerJSON(ctx, http.MethodGet, "/containers/json?all=1", &cs); err != nil {
		return nil, err
	}
	type result struct {
		local  dImageInfo
		remote string
		err    string
	}
	refs := map[string]*result{}
	for _, c := range cs {
		if imageRe.MatchString(c.Image) && !imageIDRe.MatchString(c.Image) {
			refs[c.Image] = &result{}
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for ref, res := range refs {
		wg.Add(1)
		go func(ref string, res *result) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			if err := dockerJSON(cctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", &res.local); err != nil {
				res.err = err.Error()
				return
			}
			var dist struct {
				Descriptor struct {
					Digest string `json:"digest"`
				} `json:"Descriptor"`
			}
			if err := dockerJSON(cctx, http.MethodGet, "/distribution/"+ref+"/json", &dist); err != nil {
				res.err = err.Error()
				return
			}
			res.remote = dist.Descriptor.Digest
		}(ref, res)
	}
	wg.Wait()

	entries := make(map[string]checkEntry, len(cs))
	counts := map[string]int{}
	for _, c := range cs {
		e := checkEntry{imageID: c.ImageID, status: "unknown"}
		if res, ok := refs[c.Image]; !ok {
			e.err = "the container was created from an image id"
		} else if res.err != "" {
			e.err = res.err
		} else if strings.Contains(c.Image, "@sha256:") {
			e.err = "the image is pinned by digest"
		} else {
			e = classifyImage(c.ImageID, res.local, res.remote)
		}
		entries[c.ID] = e
		counts[e.status]++
	}
	now := time.Now().Unix()
	dockerChecks.mu.Lock()
	dockerChecks.at, dockerChecks.entries = now, entries
	dockerChecks.mu.Unlock()
	return map[string]any{"checked_at": now, "new": counts["new"], "current": counts["current"], "unknown": counts["unknown"]}, nil
}

// updateMany is the stream "docker.update" with {containers: [...]}: every container one after the other.
func updateMany(ctx context.Context, ids []string, sio *StreamIO) error {
	if len(ids) == 0 || len(ids) > 100 {
		return errors.New("invalid container list")
	}
	for _, id := range ids {
		if !containerRe.MatchString(id) {
			return errors.New("invalid container")
		}
	}
	failed := 0
	for _, id := range ids {
		_ = sio.Out([]byte(fmt.Sprintf("=== %s ===\n", id)))
		if err := dockerUpdate(ctx, rawJSON(map[string]string{"container": id}), sio); err != nil {
			failed++
			_ = sio.Out([]byte(fmt.Sprintf("ERROR: %v\n", err)))
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d containers were not updated", failed, len(ids))
	}
	return nil
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// composeProjectUpdate pulls every service of the project and recreates what changed.
func composeProjectUpdate(ctx context.Context, labels map[string]string, sio *StreamIO) error {
	return composeRun(ctx, labels, sio, [][]string{{"pull"}, {"up", "-d"}}, true)
}

// composeRun runs `docker compose <steps>` in the directory of a project; each step is run in turn.
func composeRun(ctx context.Context, labels map[string]string, sio *StreamIO, steps [][]string, checkPlugin bool) error {
	bin, err := dockerCLI()
	if err != nil {
		return err
	}
	if labels[composeProject] == "" || !containerRe.MatchString(labels[composeProject]) {
		return errors.New("the compose project of the container is unknown")
	}
	run := composeRunner(ctx, bin, labels, sio)
	if checkPlugin {
		if err := run("compose", "version"); err != nil {
			return errors.New("the docker compose plugin is not available on the host")
		}
	}
	for _, s := range steps {
		if err := run(append(append([]string{}, composeArgs(labels)...), s...)...); err != nil {
			return err
		}
	}
	return nil
}

// composeRunner returns a function that runs one docker command with the output going to the stream.
func composeRunner(ctx context.Context, bin string, labels map[string]string, sio *StreamIO) func(args ...string) error {
	env := dockerEnv()
	dir := workDir(labels)
	return func(args ...string) error {
		_ = sio.Out([]byte("$ docker " + strings.Join(args, " ") + "\n"))
		cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // fixed binary, arguments are validated labels
		cmd.Env, cmd.Dir = env, dir
		w := outWriter{sio}
		cmd.Stdout, cmd.Stderr = w, w
		return cmd.Run()
	}
}

// dockerComposeLogs is the stream "docker.composeLogs": follows the logs of a whole compose project.
func dockerComposeLogs(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Container string
		Lines     int
	}
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.Container) {
		return errors.New("invalid container")
	}
	if p.Lines < 0 || p.Lines > 1000 {
		p.Lines = 200
	}
	var in dInspect
	if err := dockerJSON(ctx, http.MethodGet, "/containers/"+p.Container+"/json", &in); err != nil {
		return err
	}
	labels := stringMap(in.Config["Labels"])
	if labels[composeProject] == "" {
		return errors.New("the container does not belong to a compose project")
	}
	return composeRun(ctx, labels, sio, [][]string{{"logs", "--no-color", "--timestamps", "--tail", fmt.Sprint(p.Lines), "--follow"}}, false)
}
