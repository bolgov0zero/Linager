//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Updating a container: pull the image again and recreate the container with the same settings.
//
// A plain container is recreated through the Docker API from what `inspect` reports: the old one is
// stopped and renamed, the new one is created with the new image, started and checked; if anything
// fails the old one is brought back. A container that belongs to a compose project is updated with
// `docker compose pull` and `up -d --no-deps` for its service, so the project stays consistent.

var imageIDRe = regexp.MustCompile(`^(sha256:)?[0-9a-f]{12,64}$`)

const composeProject = "com.docker.compose.project"

type dInspect struct {
	ID              string         `json:"Id"`
	Name            string         `json:"Name"`
	Image           string         `json:"Image"` // id of the image the container runs
	Config          map[string]any `json:"Config"`
	HostConfig      map[string]any `json:"HostConfig"`
	State           struct{ Running bool }
	NetworkSettings struct {
		Networks map[string]map[string]any `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Type        string
		Name        string
		Source      string
		Destination string
		Mode        string
		RW          bool
		Propagation string
	}
}

// dockerSend calls the Docker API with an optional JSON body and decodes an optional answer.
func dockerSend(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := dockerClient(2 * time.Minute).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		var m struct{ Message string }
		if json.Unmarshal(b, &m) == nil && m.Message != "" {
			return errors.New(m.Message)
		}
		return fmt.Errorf("docker: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// dockerUpdate is the stream "docker.update": params {container, image?}.
func dockerUpdate(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Container  string
		Image      string
		Containers []string // update several containers one after the other
		Project    bool     // update the whole compose project of Container
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return errors.New("invalid parameters")
	}
	if len(p.Containers) > 0 {
		return updateMany(ctx, p.Containers, sio)
	}
	if !containerRe.MatchString(p.Container) {
		return errors.New("invalid container")
	}
	if p.Image != "" && !imageRe.MatchString(p.Image) {
		return errors.New("invalid image name")
	}
	say := func(format string, args ...any) error { return sio.Out([]byte(fmt.Sprintf(format, args...) + "\n")) }
	var old dInspect
	if err := dockerJSON(ctx, http.MethodGet, "/containers/"+p.Container+"/json", &old); err != nil {
		return err
	}
	if labels := stringMap(old.Config["Labels"]); labels[composeProject] != "" {
		if p.Project {
			return composeProjectUpdate(ctx, labels, sio)
		}
		return composeUpdate(ctx, labels, sio)
	}
	if p.Project {
		return errors.New("the container does not belong to a compose project")
	}
	return plainUpdate(ctx, &old, p.Image, say)
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			if s, ok := x.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

// plainUpdate recreates a container that was started with `docker run` (or the API).
func plainUpdate(ctx context.Context, old *dInspect, override string, say func(string, ...any) error) error {
	ref := override
	if ref == "" {
		ref, _ = old.Config["Image"].(string)
	}
	if ref == "" || imageIDRe.MatchString(ref) {
		return errors.New("the container was created from an image id, enter an image name (for example nginx:latest)")
	}
	if !imageRe.MatchString(ref) {
		return errors.New("invalid image name")
	}
	name := strings.TrimPrefix(old.Name, "/")
	_ = say("Pulling %s", ref)
	if err := pullImage(ctx, ref, func(l string) error { return say("%s", l) }); err != nil {
		return err
	}
	var img struct {
		ID string `json:"Id"`
	}
	if err := dockerJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", &img); err != nil {
		return err
	}
	if img.ID == old.Image {
		return say("The container already runs the latest %s, nothing to do.", ref)
	}
	_ = say("New image %s (was %s)", shortImageID(img.ID), shortImageID(old.Image))

	if err := swapContainer(ctx, old, name, func() (map[string]any, []extraNet) { return buildCreateBody(old, ref) }, say); err != nil {
		return err
	}
	return say("Done: %s now runs %s. The previous image %s stays on the host.", name, ref, shortImageID(old.Image))
}

// swapContainer replaces a plain container by a new one made from build(): the old one is stopped and renamed,
// the new one is created under newName, started and checked, then the old one is removed. If anything fails the
// old one is brought back under its own name.
func swapContainer(ctx context.Context, old *dInspect, newName string, build func() (map[string]any, []extraNet), say func(string, ...any) error) error {
	name := strings.TrimPrefix(old.Name, "/")
	wasRunning := old.State.Running
	oldName := fmt.Sprintf("%s-linager-old-%d", name, time.Now().Unix())
	if wasRunning {
		_ = say("Stopping %s", name)
		if err := dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/stop?t=30", nil, nil); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
	}
	if err := dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/rename?name="+url.QueryEscape(oldName), nil, nil); err != nil {
		if wasRunning {
			_ = dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/start", nil, nil)
		}
		return fmt.Errorf("rename: %w", err)
	}
	// from here on a failure puts the old container back
	restore := func(newID string) {
		if newID != "" {
			_ = dockerSend(ctx, http.MethodDelete, "/containers/"+newID+"?force=1", nil, nil)
		}
		_ = dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/rename?name="+url.QueryEscape(name), nil, nil)
		if wasRunning {
			_ = dockerSend(ctx, http.MethodPost, "/containers/"+old.ID+"/start", nil, nil)
		}
		_ = say("The old container was restored.")
	}

	body, extra := build()
	_ = say("Creating the new container")
	var created struct {
		ID string `json:"Id"`
	}
	if err := dockerSend(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(newName), body, &created); err != nil {
		restore("")
		return fmt.Errorf("create: %w", err)
	}
	for _, c := range extra {
		if err := dockerSend(ctx, http.MethodPost, "/networks/"+url.PathEscape(c.network)+"/connect", map[string]any{"Container": created.ID, "EndpointConfig": c.endpoint}, nil); err != nil {
			restore(created.ID)
			return fmt.Errorf("network %s: %w", c.network, err)
		}
	}
	if wasRunning {
		_ = say("Starting the new container")
		if err := dockerSend(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
			restore(created.ID)
			return fmt.Errorf("start: %w", err)
		}
		if !stayedRunning(ctx, created.ID) {
			restore(created.ID)
			return errors.New("the new container did not stay running")
		}
	}
	if err := dockerSend(ctx, http.MethodDelete, "/containers/"+old.ID, nil, nil); err != nil {
		_ = say("The old container %s could not be removed: %v", oldName, err)
	}
	return nil
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

// stayedRunning waits a moment and checks that the container did not exit right away.
func stayedRunning(ctx context.Context, id string) bool {
	select {
	case <-time.After(stayCheck):
	case <-ctx.Done():
		return false
	}
	var in dInspect
	return dockerJSON(ctx, http.MethodGet, "/containers/"+id+"/json", &in) == nil && in.State.Running
}

var stayCheck = 3 * time.Second

type extraNet struct {
	network  string
	endpoint map[string]any
}

// buildCreateBody makes the "create" request from the inspect data of the old container: the same
// configuration with the new image. Networks beyond the first are returned to be connected afterwards.
func buildCreateBody(old *dInspect, image string) (map[string]any, []extraNet) {
	body := map[string]any{}
	for k, v := range old.Config {
		body[k] = v
	}
	body["Image"] = image
	// the default hostname is the short id of the old container: let Docker pick one for the new
	if h, _ := body["Hostname"].(string); h != "" && strings.HasPrefix(old.ID, h) {
		delete(body, "Hostname")
	}
	host := map[string]any{}
	for k, v := range old.HostConfig {
		host[k] = v
	}
	// anonymous volumes live on in the new container
	for _, m := range old.Mounts {
		if m.Type == "volume" && len(m.Name) == 64 {
			host["VolumesFrom"] = append(toStrings(host["VolumesFrom"]), old.ID)
			break
		}
	}
	body["HostConfig"] = host

	mode, _ := host["NetworkMode"].(string)
	if mode == "" || mode == "default" {
		mode = "bridge"
	}
	var extra []extraNet
	if mode == "host" || mode == "none" || strings.HasPrefix(mode, "container:") {
		return body, nil
	}
	endpoint := func(n map[string]any) map[string]any {
		ep := map[string]any{}
		if al, ok := n["Aliases"].([]any); ok {
			var keep []any
			for _, a := range al {
				if s, _ := a.(string); s != "" && !strings.HasPrefix(old.ID, s) {
					keep = append(keep, s)
				}
			}
			if len(keep) > 0 {
				ep["Aliases"] = keep
			}
		}
		if ip, ok := n["IPAMConfig"]; ok && ip != nil {
			ep["IPAMConfig"] = ip
		}
		return ep
	}
	endpoints := map[string]any{}
	if n, ok := old.NetworkSettings.Networks[mode]; ok {
		endpoints[mode] = endpoint(n)
	}
	for _, nm := range sortedKeys(old.NetworkSettings.Networks) {
		if nm != mode {
			extra = append(extra, extraNet{network: nm, endpoint: endpoint(old.NetworkSettings.Networks[nm])})
		}
	}
	if len(endpoints) > 0 {
		body["NetworkingConfig"] = map[string]any{"EndpointsConfig": endpoints}
	}
	return body, extra
}

func toStrings(v any) []any {
	l, _ := v.([]any)
	return l
}

func sortedKeys(m map[string]map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	for i := 1; i < len(ks); i++ { // tiny, the number of networks is small
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}

// composeArgs builds the common `docker compose` arguments of a project from the labels compose put on a container.
func composeArgs(labels map[string]string) []string {
	args := []string{"compose", "--project-name", labels[composeProject]}
	if dir := labels["com.docker.compose.project.working_dir"]; dir != "" {
		args = append(args, "--project-directory", dir)
	}
	for _, f := range strings.Split(labels["com.docker.compose.project.config_files"], ",") {
		if f = strings.TrimSpace(f); f != "" {
			args = append(args, "-f", f)
		}
	}
	return args
}

// dockerCLI finds the docker binary (the agent runs with a minimal PATH).
func dockerCLI() (string, error) {
	if p, err := exec.LookPath("docker"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/bin/docker", "/usr/local/bin/docker"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("the docker command is not found on the host, it is needed to update a compose container")
}

// composeUpdate pulls and recreates the service of a compose project.
func composeUpdate(ctx context.Context, labels map[string]string, sio *StreamIO) error {
	service := labels["com.docker.compose.service"]
	if service == "" || !containerRe.MatchString(service) {
		return errors.New("the compose service of the container is unknown")
	}
	steps := [][]string{{"pull", service}, {"up", "-d", "--no-deps", service}}
	return composeRun(ctx, labels, sio, steps, true)
}

// dockerEnv is the environment for the docker command: the service runs without HOME, docker wants it for its config.
func dockerEnv() []string {
	env := os.Environ()
	if os.Getenv("HOME") == "" {
		env = append(env, "HOME=/root")
	}
	return env
}

// workDir is the directory of a compose project if it exists on the host.
func workDir(labels map[string]string) string {
	dir := labels["com.docker.compose.project.working_dir"]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

// outWriter sends what a command writes to the stream.
type outWriter struct{ sio *StreamIO }

func (w outWriter) Write(p []byte) (int, error) {
	if err := w.sio.Out(append([]byte(nil), p...)); err != nil {
		return 0, err
	}
	return len(p), nil
}
