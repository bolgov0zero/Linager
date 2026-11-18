//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Listening ports and connections for the firewall tab: what listens, who answers it, and whether a rule
// of the input chain says anything about the port. The data comes from `ss`, the rules from `nft`.

// Listener is one listening socket group (the same port on 0.0.0.0 and :: is one row).
type Listener struct {
	Proto   string   `json:"proto"` // tcp or udp
	Port    int      `json:"port"`
	Addrs   []string `json:"addrs"`
	Process string   `json:"process"`
	PID     int      `json:"pid"`
	Conns   int      `json:"conns"`
	// Scope is "all" (every interface), "local" (loopback), "addr" (one address) or "docker" (docker-proxy).
	Scope string `json:"scope"`
	// Firewall is "allowed", "restricted" (allowed only for some sources), "blocked", "open" (no rule, policy
	// accept), "policy" (no rule, policy drop), "local" or "docker".
	Firewall string `json:"firewall"`
	// Risky marks a service that should rarely be open to everybody (databases, caches, remote consoles).
	Risky bool `json:"risky"`
}

// Remote is a remote address with the number of connections to one local port.
type Remote struct {
	Addr  string `json:"addr"`
	Port  int    `json:"port"`
	Conns int    `json:"conns"`
}

var (
	ssProcRe   = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)
	riskyPorts = map[int]bool{
		23: true, 111: true, 135: true, 139: true, 445: true, 1433: true, 1521: true, 2049: true, 2375: true, 2376: true, 3306: true,
		3389: true, 5432: true, 5601: true, 5900: true, 6379: true, 9200: true, 9300: true, 11211: true, 15672: true, 27017: true,
	}
)

// splitHostPort splits the local or peer column of ss: "0.0.0.0:22", "[::]:22", "*:80", "127.0.0.53%lo:53".
func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	host := strings.Trim(s[:i], "[]")
	if j := strings.Index(host, "%"); j >= 0 {
		host = host[:j]
	}
	host = strings.TrimPrefix(host, "::ffff:")
	return host, port, true
}

func isWildcard(h string) bool { return h == "*" || h == "0.0.0.0" || h == "::" || h == "" }

func isLoopback(h string) bool {
	a, err := netip.ParseAddr(h)
	return err == nil && a.IsLoopback()
}

// parseListeners reads `ss -H -tulnp`.
func parseListeners(out string) []Listener {
	byKey := map[string]*Listener{}
	var order []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || (f[0] != "tcp" && f[0] != "udp") {
			continue
		}
		host, port, ok := splitHostPort(f[4])
		if !ok || port == 0 {
			continue
		}
		l := Listener{Proto: f[0], Port: port, Addrs: []string{host}, Scope: "addr"}
		if m := ssProcRe.FindStringSubmatch(line); m != nil {
			l.Process = m[1]
			l.PID, _ = strconv.Atoi(m[2])
		}
		switch {
		case isWildcard(host):
			l.Scope = "all"
			l.Addrs = []string{map[bool]string{true: "::", false: "0.0.0.0"}[strings.Contains(f[4], "::") || host == "::"]}
			if host == "*" {
				l.Addrs = []string{"0.0.0.0"}
			}
		case isLoopback(host):
			l.Scope = "local"
		}
		if l.Process == "docker-proxy" && l.Scope != "local" {
			l.Scope = "docker"
		}
		key := l.Proto + "/" + strconv.Itoa(port) + "/" + l.Scope + "/" + l.Process
		if l.Scope == "addr" {
			key += "/" + host
		}
		if prev, ok := byKey[key]; ok {
			for _, a := range l.Addrs {
				if !contains(prev.Addrs, a) {
					prev.Addrs = append(prev.Addrs, a)
				}
			}
			continue
		}
		l.Risky = riskyPorts[port]
		byKey[key] = &l
		order = append(order, key)
	}
	res := make([]Listener, 0, len(order))
	for _, k := range order {
		res = append(res, *byKey[k])
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Port != res[j].Port {
			return res[i].Port < res[j].Port
		}
		return res[i].Proto < res[j].Proto
	})
	return res
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// parseConnections reads `ss -H -tn state established`: connections per local port and the remote addresses.
func parseConnections(out string) (perPort map[int]int, remotes []Remote) {
	perPort = map[int]int{}
	type rk struct {
		addr string
		port int
	}
	counts := map[rk]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		// without the state column: Recv-Q Send-Q Local Peer
		if len(f) < 4 {
			continue
		}
		_, lp, ok1 := splitHostPort(f[len(f)-2])
		rh, _, ok2 := splitHostPort(f[len(f)-1])
		if !ok1 || !ok2 {
			continue
		}
		perPort[lp]++
		counts[rk{rh, lp}]++
	}
	for k, n := range counts {
		remotes = append(remotes, Remote{Addr: k.addr, Port: k.port, Conns: n})
	}
	sort.Slice(remotes, func(i, j int) bool {
		if remotes[i].Conns != remotes[j].Conns {
			return remotes[i].Conns > remotes[j].Conns
		}
		return remotes[i].Addr < remotes[j].Addr
	})
	return perPort, remotes
}

var (
	dportRe   = regexp.MustCompile(`\b(tcp|udp) dport (\{[^}]*\}|[0-9]+(?:-[0-9]+)?)`)
	commentRe = regexp.MustCompile(`comment "[^"]*"`)
)

// portMatches reports whether the dport expression ("22", "1000-2000", "{ 80, 443 }") covers the port.
func portMatches(expr string, port int) bool {
	expr = strings.Trim(expr, "{} ")
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, e1 := strconv.Atoi(strings.TrimSpace(lo))
			b, e2 := strconv.Atoi(strings.TrimSpace(hi))
			if e1 == nil && e2 == nil && port >= a && port <= b {
				return true
			}
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n == port {
			return true
		}
	}
	return false
}

// ruleVerdict looks for the first rule of a chain that names the port. It returns "allowed", "restricted"
// (accepted only for some source addresses), "blocked" or "" when no rule mentions the port.
func ruleVerdict(rules []FwRule, proto string, port int) string {
	for _, r := range rules {
		text := commentRe.ReplaceAllString(r.Text, "")
		m := dportRe.FindStringSubmatch(text)
		if m == nil || m[1] != proto || !portMatches(m[2], port) {
			continue
		}
		f := strings.Fields(text)
		if len(f) == 0 {
			continue
		}
		switch f[len(f)-1] {
		case "accept":
			if strings.Contains(text, " saddr ") || strings.Contains(text, "iifname") {
				return "restricted"
			}
			return "allowed"
		case "drop", "reject":
			if strings.Contains(text, " saddr ") {
				continue // blocks only some sources: look further
			}
			return "blocked"
		}
	}
	return ""
}

// classify fills the Firewall field of the listeners from the input chain.
func classify(ls []Listener, input FwChain) {
	for i := range ls {
		l := &ls[i]
		switch l.Scope {
		case "local":
			l.Firewall = "local"
			continue
		case "docker":
			l.Firewall = "docker"
			continue
		}
		switch v := ruleVerdict(input.Rules, l.Proto, l.Port); {
		case v != "":
			l.Firewall = v
		case input.Policy == "drop":
			l.Firewall = "policy"
		default:
			l.Firewall = "open"
		}
	}
}

func (m *fwModule) ports(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	if _, err := exec.LookPath("ss"); err != nil {
		return map[string]any{"available": false, "listeners": []Listener{}, "remotes": []Remote{}}, nil
	}
	out, err := m.run(ctx, "ss", "-H", "-tulnp")
	if err != nil {
		return nil, err
	}
	ls := parseListeners(string(out))
	var perPort map[int]int
	var remotes []Remote
	if est, err := m.run(ctx, "ss", "-H", "-tn", "state", "established"); err == nil {
		perPort, remotes = parseConnections(string(est))
	}
	var input FwChain
	haveRules := false
	for _, c := range parseFilterTable(m.currentTable(ctx)) {
		if c.Name == "input" {
			input, haveRules = c, true
		}
	}
	if !haveRules {
		input = FwChain{Name: "input", Policy: "accept"}
	}
	classify(ls, input)
	exposed := 0
	for i := range ls {
		if ls[i].Proto == "tcp" {
			ls[i].Conns = perPort[ls[i].Port]
		}
		if ls[i].Firewall == "open" {
			exposed++
		}
	}
	top := make([]Remote, 0, 8)
	listening := map[int]bool{}
	for _, l := range ls {
		listening[l.Port] = true
	}
	for _, r := range remotes {
		if len(top) >= 8 {
			break
		}
		if listening[r.Port] && !isLoopback(r.Addr) {
			top = append(top, r)
		}
	}
	return map[string]any{"available": true, "policy": input.Policy, "listeners": ls, "remotes": top, "exposed": exposed}, nil
}
