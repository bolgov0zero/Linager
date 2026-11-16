package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DNS editing (network.dns). The agent writes to the place the host really takes its resolver settings from:
//
//	resolved    /etc/resolv.conf points into systemd-resolved: a drop-in resolved.conf.d/linager.conf
//	resolvconf  the resolvconf package owns resolv.conf: dns-nameservers / dns-search of the primary stanza
//	file        a plain /etc/resolv.conf: rewritten atomically, unrelated lines are kept
//
// The change goes through the same pending flow as network.apply: the old content is kept in the pending data
// and comes back unless the administrator confirms in time.
const (
	dnsResolved   = "resolved"
	dnsResolvconf = "resolvconf"
	dnsFile       = "file"

	resolvedUpstreamPath = "/run/systemd/resolve/resolv.conf"
	resolvedDropIn       = "linager.conf"

	maxDNSServers = 3
	maxDNSSearch  = 6
	// maxSearchLine is the glibc limit of a search line.
	maxSearchLine = 256
)

// stdinRunner runs a command with text on its standard input.
type stdinRunner func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)

func execStdinRunner(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass fixed commands and validated arguments
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.CombinedOutput()
}

// dnsPending is what a DNS change keeps to undo itself.
type dnsPending struct {
	Backend string `json:"backend"`
	// resolved, file: the file that was written and its previous content
	Path    string `json:"path,omitempty"`
	Old     string `json:"old,omitempty"`
	Existed bool   `json:"existed,omitempty"`
	// resolvconf: the interface whose stanza was changed and the dns lines it had
	Iface     string   `json:"iface,omitempty"`
	OldNS     []string `json:"old_ns,omitempty"`
	OldSearch []string `json:"old_search,omitempty"`
	HadDNS    bool     `json:"had_dns,omitempty"`
}

// ---- validation and rendering (pure) ----

var hostLabelRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if !hostLabelRe.MatchString(l) {
			return false
		}
	}
	return true
}

// validateDNS checks and normalizes the lists: 1 to 3 IPv4/IPv6 literals, at most 6 search domains.
func validateDNS(ns, search []string) ([]string, []string, error) {
	var outNS, outSearch []string
	seen := map[string]bool{}
	for _, s := range ns {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, nil, fmt.Errorf("%q is not an IPv4 or IPv6 address", s)
		}
		n := ip.String()
		if !seen[n] {
			seen[n] = true
			outNS = append(outNS, n)
		}
	}
	if len(outNS) == 0 {
		return nil, nil, errors.New("at least one name server is required")
	}
	if len(outNS) > maxDNSServers {
		return nil, nil, fmt.Errorf("at most %d name servers are allowed", maxDNSServers)
	}
	seen = map[string]bool{}
	for _, s := range search {
		s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
		if s == "" {
			continue
		}
		if !validHostname(s) {
			return nil, nil, fmt.Errorf("%q is not a valid domain name", s)
		}
		if !seen[s] {
			seen[s] = true
			outSearch = append(outSearch, s)
		}
	}
	if len(outSearch) > maxDNSSearch {
		return nil, nil, fmt.Errorf("at most %d search domains are allowed", maxDNSSearch)
	}
	if len("search "+strings.Join(outSearch, " ")) > maxSearchLine && len(outSearch) > 0 {
		return nil, nil, errors.New("the search domains are too long (256 characters at most)")
	}
	return outNS, outSearch, nil
}

type resolvConf struct{ ns, search []string }

// parseResolvConf reads the nameserver and search/domain lines of a resolv.conf.
func parseResolvConf(text string) resolvConf {
	var rc resolvConf
	for _, l := range strings.Split(text, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "nameserver" {
			rc.ns = append(rc.ns, f[1])
		} else if len(f) >= 2 && (f[0] == "search" || f[0] == "domain") {
			rc.search = append(rc.search, f[1:]...)
		}
	}
	return rc
}

// renderResolvConf replaces the nameserver and search/domain lines of a resolv.conf with the new lists. The
// first old line of each kind keeps its place; options, comments and everything else stay as they were.
func renderResolvConf(old string, ns, search []string) string {
	nsLines := make([]string, 0, len(ns))
	for _, n := range ns {
		nsLines = append(nsLines, "nameserver "+n)
	}
	searchLine := ""
	if len(search) > 0 {
		searchLine = "search " + strings.Join(search, " ")
	}
	var out []string
	nsDone, searchDone := false, false
	for _, l := range strings.Split(strings.TrimRight(old, "\n"), "\n") {
		if old == "" {
			break
		}
		switch firstWord(l) {
		case "nameserver":
			if !nsDone {
				out = append(out, nsLines...)
				nsDone = true
			}
		case "search", "domain":
			if !searchDone {
				if searchLine != "" {
					out = append(out, searchLine)
				}
				searchDone = true
			}
		default:
			out = append(out, l)
		}
	}
	if !nsDone {
		out = append(out, nsLines...)
	}
	if !searchDone && searchLine != "" {
		out = append(out, searchLine)
	}
	return strings.Join(out, "\n") + "\n"
}

// renderResolvedDropIn is the content of resolved.conf.d/linager.conf.
func renderResolvedDropIn(ns, search []string) string {
	var b strings.Builder
	b.WriteString("# Managed by Linager (Network, DNS). Changes made here are overwritten by the panel.\n[Resolve]\n")
	b.WriteString("DNS=" + strings.Join(ns, " ") + "\n")
	if len(search) > 0 {
		b.WriteString("Domains=" + strings.Join(search, " ") + "\n")
	}
	return b.String()
}

var stanzaDNSOpts = map[string]bool{"dns-nameservers": true, "dns-nameserver": true, "dns-search": true}

// stanzaDNS returns the dns-* values of the inet stanza of iface and whether it has any.
func stanzaDNS(text, iface string) (ns, search []string, has bool) {
	for _, b := range parseIfaceBlocks(strings.Split(text, "\n")) {
		if b.name != iface || b.family != "inet" {
			continue
		}
		for _, o := range b.opts {
			f := strings.Fields(o)
			if len(f) < 2 || !stanzaDNSOpts[f[0]] {
				continue
			}
			has = true
			if f[0] == "dns-search" {
				search = append(search, f[1:]...)
			} else {
				ns = append(ns, f[1:]...)
			}
		}
		return ns, search, has
	}
	return nil, nil, false
}

// setStanzaDNS rewrites the dns-nameservers / dns-search lines of the inet stanza of iface and leaves every
// other line of the file alone.
func setStanzaDNS(text, iface string, ns, search []string) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var blk *ifaceBlock
	for _, b := range parseIfaceBlocks(lines) {
		if b.name == iface && b.family == "inet" {
			blk = b
			break
		}
	}
	if blk == nil {
		return "", fmt.Errorf("no iface %s inet stanza in the interfaces file", iface)
	}
	indent := "    "
	for _, o := range blk.opts {
		if t := strings.TrimLeft(o, " \t"); t != o {
			indent = o[:len(o)-len(t)]
			break
		}
	}
	res := append([]string{}, lines[:blk.start]...)
	for _, l := range lines[blk.start:blk.end] {
		if !stanzaDNSOpts[firstWord(l)] {
			res = append(res, l)
		}
	}
	res = append(res, indent+"dns-nameservers "+strings.Join(ns, " "))
	if len(search) > 0 {
		res = append(res, indent+"dns-search "+strings.Join(search, " "))
	}
	res = append(res, lines[blk.end:]...)
	return strings.Join(res, "\n") + "\n", nil
}

// ---- choosing the backend (pure) ----

// dnsProbe is what the host tells about its resolver set-up.
type dnsProbe struct {
	Netplan       bool   // netplan manages the network: DNS editing is off, like interface editing
	IfUpdown      bool   // /etc/network/interfaces is the active backend
	Link          bool   // /etc/resolv.conf is a symlink
	Target        string // where it points
	ResolvconfBin bool   // /sbin/resolvconf exists
	StanzaFound   bool   // the primary interface has an editable inet stanza in the main interfaces file
	StanzaHasDNS  bool   // ... with dns-* lines
}

func isResolvedTarget(t string) bool {
	return strings.Contains(t, "systemd/resolve") || strings.HasSuffix(t, "stub-resolv.conf")
}

// pickDNSBackend names the place DNS settings are written to and whether editing is allowed at all.
func pickDNSBackend(p dnsProbe) (backend string, editable bool) {
	switch {
	case p.Netplan:
		return "netplan", false
	case p.Link && isResolvedTarget(p.Target):
		return dnsResolved, true
	case p.ResolvconfBin && p.IfUpdown && ((p.Link && strings.Contains(p.Target, "resolvconf")) || p.StanzaHasDNS):
		return dnsResolvconf, p.StanzaFound
	case !p.Link:
		return dnsFile, true
	default:
		return "unknown", false
	}
}

func resolvconfBin() bool {
	for _, p := range []string{"/sbin/resolvconf", "/usr/sbin/resolvconf"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// dnsProbe gathers the facts; text and blocks are the interfaces file (ifupdown only), primary the default route's device.
func (m *netModule) dnsProbe(backend, primary, text string, blocks []*ifaceBlock) dnsProbe {
	p := dnsProbe{Netplan: backend == "netplan", IfUpdown: backend == "ifupdown", ResolvconfBin: resolvconfBin()}
	if fi, err := os.Lstat(m.resolvPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		p.Link = true
		p.Target, _ = os.Readlink(m.resolvPath)
	}
	if p.IfUpdown && primary != "" && !definedInIncludes(text, primary) {
		for _, b := range blocks {
			if b.name == primary && b.family == "inet" {
				p.StanzaFound = true
			}
		}
		_, _, p.StanzaHasDNS = stanzaDNS(text, primary)
	}
	return p
}

// primaryDev is the device of the default route.
func (m *netModule) primaryDev(ctx context.Context) string {
	var routes []ipRoute
	if out, err := m.run(ctx, "ip", "-j", "route", "show"); err == nil {
		_ = json.Unmarshal(out, &routes)
	}
	for _, r := range routes {
		if r.Dst == "default" {
			return r.Dev
		}
	}
	return ""
}

// ---- files ----

// writeFileAtomic writes a temp file next to path and renames it over. A bind-mounted file (containers)
// cannot be replaced by a rename, so it is overwritten in place as a fallback.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".linager-new"
	if err := os.WriteFile(tmp, data, mode); err != nil { //nolint:gosec // a fixed configuration path
		return err
	}
	_ = os.Chmod(tmp, mode)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if werr := os.WriteFile(path, data, mode); werr != nil { //nolint:gosec // a fixed configuration path
			return fmt.Errorf("%w (in place: %w)", err, werr)
		}
	}
	return nil
}

func (m *netModule) restartResolved(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if out, err := m.run(cctx, "systemctl", "restart", "systemd-resolved"); err != nil {
		return fmt.Errorf("systemctl restart systemd-resolved: %s", lastLine(string(out), err.Error()))
	}
	return nil
}

func (m *netModule) resolvconfAdd(ctx context.Context, iface string, ns, search []string) error {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	text := strings.TrimSuffix(renderResolvConf("", ns, search), "\n") + "\n"
	if out, err := m.runStdin(cctx, text, "resolvconf", "-a", iface+".inet"); err != nil {
		return fmt.Errorf("resolvconf -a: %s", lastLine(string(out), err.Error()))
	}
	return nil
}

// ---- the command ----

// dns changes the name servers and search domains: network.dns {id, nameservers, search}.
func (m *netModule) dns(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		ID          string   `json:"id"`
		Nameservers []string `json:"nameservers"`
		Search      []string `json:"search"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" {
		return nil, errors.New("invalid request")
	}
	ns, search, err := validateDNS(p.Nameservers, p.Search)
	if err != nil {
		return nil, err
	}
	backend := m.backend()
	primary := m.primaryDev(ctx)
	text := ""
	var blocks []*ifaceBlock
	if backend == "ifupdown" {
		text = read(m.ifPath)
		blocks = parseIfaceBlocks(strings.Split(text, "\n"))
	}
	kind, editable := pickDNSBackend(m.dnsProbe(backend, primary, text, blocks))
	if !editable {
		return nil, fmt.Errorf("DNS editing is not available on this system (%s)", kind)
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	dp := &dnsPending{Backend: kind}
	data := netPendingData{DNS: dp}
	var apply func() error
	switch kind {
	case dnsResolved:
		dp.Path = filepath.Join(m.resolvedD, resolvedDropIn)
		old, rerr := os.ReadFile(dp.Path) //nolint:gosec // fixed drop-in name
		dp.Existed, dp.Old = rerr == nil, string(old)
		content := renderResolvedDropIn(ns, search)
		if dp.Existed && dp.Old == content {
			return nil, errors.New("nothing changed")
		}
		apply = func() error {
			if err := os.MkdirAll(m.resolvedD, 0o755); err != nil { //nolint:gosec // resolved reads the drop-in directory as any user
				return err
			}
			if err := writeFileAtomic(dp.Path, []byte(content), 0o644); err != nil {
				return err
			}
			return m.restartResolved(ctx)
		}
	case dnsFile:
		dp.Path = m.resolvPath
		old, rerr := os.ReadFile(dp.Path) //nolint:gosec // /etc/resolv.conf
		dp.Existed, dp.Old = rerr == nil, string(old)
		if len(old) > maxRawConfig {
			return nil, errors.New("resolv.conf is too large to edit")
		}
		content := renderResolvConf(dp.Old, ns, search)
		if dp.Existed && strings.TrimRight(dp.Old, "\n")+"\n" == content {
			return nil, errors.New("nothing changed")
		}
		apply = func() error { return writeFileAtomic(dp.Path, []byte(content), 0o644) }
	case dnsResolvconf:
		if definedInIncludes(text, primary) {
			return nil, fmt.Errorf("%s is defined in an included file; edit it manually", primary)
		}
		cand, serr := setStanzaDNS(text, primary, ns, search)
		if serr != nil {
			return nil, serr
		}
		if cand == strings.TrimRight(text, "\n")+"\n" {
			return nil, errors.New("nothing changed")
		}
		dp.Iface = primary
		dp.OldNS, dp.OldSearch, dp.HadDNS = stanzaDNS(text, primary)
		data.Cand = filepath.Join(m.dir, "net-"+p.ID+".interfaces")
		data.Old = m.ifPath
		if err := os.WriteFile(data.Cand, []byte(cand), 0o600); err != nil {
			return nil, err
		}
		apply = func() error { return m.resolvconfAdd(ctx, primary, ns, search) }
	default:
		return nil, errors.New("DNS editing is not available on this system")
	}
	deadline, err := m.pend.Begin(p.ID, "network", data, PendingWindow)
	if err != nil {
		if data.Cand != "" {
			_ = os.Remove(data.Cand)
		}
		return nil, err
	}
	if err := apply(); err != nil {
		m.pend.Abort(p.ID) // restores whatever was already written
		return nil, err
	}
	return map[string]any{"id": p.ID, "deadline": deadline}, nil
}

// dnsRollback restores the settings that were in place before the change.
func (m *netModule) dnsRollback(d netPendingData) error {
	dp := d.DNS
	ctx := context.Background()
	switch dp.Backend {
	case dnsResolved:
		if dp.Existed {
			if err := writeFileAtomic(dp.Path, []byte(dp.Old), 0o644); err != nil {
				return err
			}
		} else if err := os.Remove(dp.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return m.restartResolved(ctx)
	case dnsFile:
		if !dp.Existed {
			_ = os.Remove(dp.Path)
			return nil
		}
		return writeFileAtomic(dp.Path, []byte(dp.Old), 0o644)
	case dnsResolvconf:
		defer func() { _ = os.Remove(d.Cand) }()
		if dp.Iface == "" || strings.ContainsAny(dp.Iface, " /\t\n") {
			return errors.New("invalid interface in the pending change")
		}
		if dp.HadDNS && len(dp.OldNS) > 0 {
			return m.resolvconfAdd(ctx, dp.Iface, dp.OldNS, dp.OldSearch)
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if out, err := m.run(cctx, "resolvconf", "-d", dp.Iface+".inet"); err != nil {
			return fmt.Errorf("resolvconf -d: %s", lastLine(string(out), err.Error()))
		}
	}
	return nil
}
