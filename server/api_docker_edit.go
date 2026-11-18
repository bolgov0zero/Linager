package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/server/store"
)

// Editing a Docker container. The browser first POSTs the wanted settings (they may hold passwords in the
// variables, so they never travel in a URL) and gets a one-time token; the stream "docker-edit" carries only
// that token, the server hands the settings to the agent. The audit log gets the names of the variables, not
// their values.

const (
	editJobTTL  = 2 * time.Minute
	editMaxBody = 1 << 20
)

var dockerIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type dockerEditSpec struct {
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	Restart string  `json:"restart"`
	Retries int     `json:"retries"`
	Cmd     string  `json:"cmd"`
	MemMB   int     `json:"mem_mb"`
	CPUs    float64 `json:"cpus"`
	Ports   []struct {
		IP    string `json:"ip"`
		Host  int    `json:"host"`
		Cont  int    `json:"cont"`
		Proto string `json:"proto"`
	} `json:"ports"`
	Mounts []struct {
		Type string `json:"type"`
		Src  string `json:"src"`
		Dst  string `json:"dst"`
		RO   bool   `json:"ro"`
	} `json:"mounts"`
	Env []struct {
		K string `json:"k"`
		V string `json:"v"`
	} `json:"env"`
}

type dockerEditJob struct {
	server    string
	container string
	spec      dockerEditSpec
	exp       time.Time
}

type dockerEditJobs struct {
	mu   sync.Mutex
	jobs map[string]*dockerEditJob
}

func (j *dockerEditJobs) put(job *dockerEditJob) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.jobs == nil {
		j.jobs = map[string]*dockerEditJob{}
	}
	now := time.Now()
	for k, v := range j.jobs { // forget the jobs nobody started
		if now.After(v.exp) {
			delete(j.jobs, k)
		}
	}
	job.exp = now.Add(editJobTTL)
	j.jobs[tok] = job
	return tok
}

// take returns the job once: a token works for one stream of the same server.
func (j *dockerEditJobs) take(server, tok string) *dockerEditJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	job := j.jobs[tok]
	if job == nil || job.server != server || time.Now().After(job.exp) {
		return nil
	}
	delete(j.jobs, tok)
	return job
}

func (a *Admin) dockerConfig(w http.ResponseWriter, r *http.Request, _ *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	cid := r.PathValue("cid")
	if !dockerIDRe.MatchString(cid) {
		jsonErr(w, http.StatusBadRequest, "invalid container")
		return
	}
	data, err := a.Hub.Call(r.Context(), s.ID, "docker", "config", map[string]string{"id": cid}, false, 30*time.Second)
	if err != nil {
		callErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (a *Admin) dockerEdit(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	cid := r.PathValue("cid")
	if !dockerIDRe.MatchString(cid) {
		jsonErr(w, http.StatusBadRequest, "invalid container")
		return
	}
	var req struct {
		Spec    dockerEditSpec `json:"spec"`
		Confirm bool           `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, editMaxBody)).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	sp := req.Spec
	if len(sp.Ports) > 200 || len(sp.Mounts) > 200 || len(sp.Env) > 500 {
		jsonErr(w, http.StatusBadRequest, "too many ports, mounts or variables")
		return
	}
	tok := a.editJobs.put(&dockerEditJob{server: s.ID, container: cid, spec: sp})
	ports := make([]string, 0, len(sp.Ports))
	for _, p := range sp.Ports {
		ports = append(ports, p.IP+":"+strconv.Itoa(p.Host)+"->"+strconv.Itoa(p.Cont)+"/"+p.Proto)
	}
	mounts := make([]string, 0, len(sp.Mounts))
	for _, m := range sp.Mounts {
		mode := "rw"
		if m.RO {
			mode = "ro"
		}
		mounts = append(mounts, m.Type+":"+m.Src+":"+m.Dst+":"+mode)
	}
	names := make([]string, 0, len(sp.Env))
	for _, e := range sp.Env {
		names = append(names, e.K)
	}
	sort.Strings(names)
	a.audit(r, u, "docker.container.edit", "server", s.ID, s.Name, "ok", map[string]any{"container": cid, "name": sp.Name,
		"image": sp.Image, "ports": ports, "mounts": mounts, "env_names": names})
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}
