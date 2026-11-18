package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

// The lists of ignored updates: packages (apt) and containers (Docker). An ignored item stays in the lists of the
// panel, but does not count towards the update numbers of the dashboard and of the side menu. The lists are kept in
// the data directory of the agent, so the counting happens where the full list of updates is known.

type ignoreList struct {
	mu     sync.Mutex
	path   string
	Apt    map[string]bool `json:"apt"`
	Docker map[string]bool `json:"docker"`
}

var ignores = &ignoreList{Apt: map[string]bool{}, Docker: map[string]bool{}}

var (
	ignoreAptRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+.:_-]{0,127}$`)
	ignoreDockerRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

const maxIgnored = 500

// loadIgnores reads the lists from the data directory; a missing or broken file means empty lists.
func loadIgnores(dir string) {
	l := &ignoreList{path: filepath.Join(dir, "ignored.json"), Apt: map[string]bool{}, Docker: map[string]bool{}}
	if b, err := os.ReadFile(l.path); err == nil { //nolint:gosec // our own file
		var f struct {
			Apt    []string `json:"apt"`
			Docker []string `json:"docker"`
		}
		if json.Unmarshal(b, &f) == nil {
			for _, n := range f.Apt {
				if ignoreAptRe.MatchString(n) {
					l.Apt[n] = true
				}
			}
			for _, n := range f.Docker {
				if ignoreDockerRe.MatchString(n) {
					l.Docker[n] = true
				}
			}
		}
	}
	ignores = l
}

func (l *ignoreList) isApt(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Apt[name]
}

func (l *ignoreList) isDocker(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Docker[name]
}

func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// set adds a name to the list of a kind ("apt" or "docker") or removes it, and writes the file (mode 600).
func (l *ignoreList) set(kind, name string, on bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var m map[string]bool
	switch kind {
	case "apt":
		if !ignoreAptRe.MatchString(name) {
			return errors.New("invalid package name")
		}
		m = l.Apt
	case "docker":
		if !ignoreDockerRe.MatchString(name) {
			return errors.New("invalid container name")
		}
		m = l.Docker
	default:
		return errors.New("invalid kind")
	}
	if on {
		if len(m) >= maxIgnored {
			return errors.New("too many ignored items")
		}
		m[name] = true
	} else {
		delete(m, name)
	}
	if l.path == "" {
		return nil
	}
	b, err := json.Marshal(map[string][]string{"apt": sortedNames(l.Apt), "docker": sortedNames(l.Docker)})
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// ignoreCmd is "apt.ignore" and "docker.ignore": {name, ignore}. after recounts what the dashboard shows.
func ignoreCmd(kind string, after func()) CmdFunc {
	return func(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
		var p struct {
			Name   string `json:"name"`
			Ignore bool   `json:"ignore"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errors.New("invalid request")
		}
		if err := ignores.set(kind, p.Name, p.Ignore); err != nil {
			return nil, err
		}
		if after != nil {
			after()
		}
		return map[string]bool{"ok": true}, nil
	}
}
