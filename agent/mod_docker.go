package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// dockerSocket is a variable so tests can point it at a fake daemon.
var dockerSocket = "/var/run/docker.sock"

var (
	containerRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	imageRe     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./:@-]{0,255}$`)
)

// dockerAvailable reports whether the Docker socket exists.
func dockerAvailable() bool {
	_, err := os.Stat(dockerSocket)
	return err == nil
}

func dockerClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
		}}}
}

func dockerDo(ctx context.Context, c *http.Client, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func dockerJSON(ctx context.Context, method, path string, out any) error {
	resp, err := dockerDo(ctx, dockerClient(30*time.Second), method, path)
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

type dContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

type dImage struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Size     int64    `json:"Size"`
	Created  int64    `json:"Created"`
}

type dStats struct {
	CPUStats struct {
		CPUUsage struct {
			Total uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		System uint64 `json:"system_cpu_usage"`
		Online int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPU struct {
		CPUUsage struct {
			Total uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		System uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	Memory struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

func cpuPercent(s dStats) float64 {
	cd := float64(s.CPUStats.CPUUsage.Total) - float64(s.PreCPU.CPUUsage.Total)
	sd := float64(s.CPUStats.System) - float64(s.PreCPU.System)
	if cd <= 0 || sd <= 0 {
		return 0
	}
	n := float64(s.CPUStats.Online)
	if n == 0 {
		n = 1
	}
	return cd / sd * n * 100
}

func portStrings(c dContainer) []string {
	out := []string{}
	for _, p := range c.Ports {
		if p.PublicPort > 0 {
			out = append(out, fmt.Sprintf("%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type))
		} else {
			out = append(out, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		}
	}
	return out
}

func dockerState(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	if !dockerAvailable() {
		return map[string]any{"available": false, "containers": []any{}, "images": []any{}}, nil
	}
	var cs []dContainer
	if err := dockerJSON(ctx, http.MethodGet, "/containers/json?all=1", &cs); err != nil {
		return nil, err
	}
	var ims []dImage
	if err := dockerJSON(ctx, http.MethodGet, "/images/json", &ims); err != nil {
		return nil, err
	}
	used := map[string]bool{}
	type row struct {
		ID, Name, Image, State, Status string
		Ports                          []string
		Compose                        bool
		Project, Service, Dir, File    string
		ImageID, FullID                string
		Created                        int64
		CPU                            float64
		MemUsed, MemLimit              uint64
	}
	rows := make([]*row, len(cs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, c := range cs {
		used[c.ImageID] = true
		name := strings.TrimPrefix(first(c.Names), "/")
		rows[i] = &row{ID: c.ID[:12], Name: name, Image: c.Image, State: c.State, Status: c.Status, Ports: portStrings(c), Created: c.Created, Compose: c.Labels[composeProject] != "",
			Project: c.Labels[composeProject], Service: c.Labels["com.docker.compose.service"], Dir: c.Labels["com.docker.compose.project.working_dir"],
			File: first(strings.Split(c.Labels["com.docker.compose.project.config_files"], ",")), ImageID: c.ImageID, FullID: c.ID}
		if c.State != "running" {
			continue
		}
		wg.Add(1)
		go func(r *row, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sctx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			var s dStats
			if dockerJSON(sctx, http.MethodGet, "/containers/"+id+"/stats?stream=false", &s) == nil {
				r.CPU = cpuPercent(s)
				r.MemUsed = s.Memory.Usage - min(s.Memory.Usage, s.Memory.Stats.InactiveFile)
				r.MemLimit = s.Memory.Limit
			}
		}(rows[i], c.ID)
	}
	wg.Wait()
	cont := make([]map[string]any, 0, len(rows))
	valid := map[string]checkEntry{}
	for _, r := range rows {
		m := map[string]any{"id": r.ID, "name": r.Name, "image": r.Image, "state": r.State, "status": r.Status, "ports": r.Ports,
			"created": r.Created, "compose": r.Compose, "cpu": r.CPU, "mem_used": r.MemUsed, "mem_limit": r.MemLimit}
		if r.Compose {
			m["project"], m["service"], m["workdir"], m["config_file"] = r.Project, r.Service, r.Dir, r.File
		}
		if e, ok := checkFor(r.FullID, r.ImageID); ok {
			m["upd"], m["upd_local"], m["upd_remote"], m["upd_error"] = e.status, e.local, e.remote, e.err
			valid[r.FullID] = e
		}
		cont = append(cont, m)
	}
	syncImageUpdates(valid)
	sort.Slice(cont, func(i, j int) bool { return cont[i]["name"].(string) < cont[j]["name"].(string) })
	imgs := make([]map[string]any, 0, len(ims))
	for _, im := range ims {
		tags := im.RepoTags
		if tags == nil {
			tags = []string{}
		}
		imgs = append(imgs, map[string]any{"id": strings.TrimPrefix(im.ID, "sha256:")[:12], "repo_tags": tags, "size": im.Size, "created": im.Created, "used": used[im.ID]})
	}
	return map[string]any{"available": true, "containers": cont, "images": imgs, "checked_at": checkedAt()}, nil
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func dockerContainerAction(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct{ ID, Action string }
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.ID) {
		return nil, errors.New("invalid container")
	}
	var err error
	switch p.Action {
	case "start", "stop", "restart":
		err = dockerJSON(ctx, http.MethodPost, "/containers/"+p.ID+"/"+p.Action, nil)
	case "remove":
		if !dangerous {
			return nil, errDangerous
		}
		err = dockerJSON(ctx, http.MethodDelete, "/containers/"+p.ID+"?force=1", nil)
	default:
		return nil, errors.New("invalid action")
	}
	if err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

func dockerImageRemove(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ ID string }
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.ID) {
		return nil, errors.New("invalid image")
	}
	if err := dockerJSON(ctx, http.MethodDelete, "/images/"+p.ID, nil); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}

// dockerLogs follows container logs, demultiplexing the stdout/stderr framing.
func dockerLogs(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Container string `json:"container"`
		Lines     int    `json:"lines"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || !containerRe.MatchString(p.Container) {
		return errors.New("invalid container")
	}
	if p.Lines < 0 || p.Lines > 1000 {
		p.Lines = 200
	}
	var info struct{ Config struct{ Tty bool } }
	if err := dockerJSON(ctx, http.MethodGet, "/containers/"+p.Container+"/json", &info); err != nil {
		return err
	}
	resp, err := dockerDo(ctx, dockerClient(0), http.MethodGet, fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&follow=1&tail=%d", p.Container, p.Lines))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("docker: %s", resp.Status)
	}
	return copyDockerStream(resp.Body, info.Config.Tty, sio)
}

func copyDockerStream(r io.Reader, tty bool, sio *StreamIO) error {
	br := bufio.NewReaderSize(r, 32<<10)
	if tty {
		buf := make([]byte, 8<<10)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
					return oerr
				}
			}
			if err != nil {
				return nil
			}
		}
	}
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			return nil
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		if size > 1<<20 {
			return errors.New("unexpected log frame")
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(br, frame); err != nil {
			return nil
		}
		if err := sio.Out(frame); err != nil {
			return err
		}
	}
}

// dockerPull pulls an image and streams a readable progress log.
func dockerPull(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct{ Image string }
	if err := json.Unmarshal(raw, &p); err != nil || !imageRe.MatchString(p.Image) {
		return errors.New("invalid image name")
	}
	return pullImage(ctx, p.Image, func(line string) error { return sio.Out([]byte(line + "\n")) })
}

// pullImage pulls an image and reports the progress lines to out.
func pullImage(ctx context.Context, image string, out func(string) error) error {
	repo, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") && !strings.Contains(image[i:], "@") {
		repo, tag = image[:i], image[i+1:]
	}
	q := url.Values{"fromImage": {repo}, "tag": {tag}}
	resp, err := dockerDo(ctx, dockerClient(0), http.MethodPost, "/images/create?"+q.Encode())
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
	dec := json.NewDecoder(resp.Body)
	for {
		var m struct{ Status, ID, Progress, Error string }
		if err := dec.Decode(&m); err != nil {
			return nil
		}
		if m.Error != "" {
			return errors.New(m.Error)
		}
		if err := out(strings.TrimSpace(m.ID + " " + m.Status + " " + m.Progress)); err != nil {
			return err
		}
	}
}
