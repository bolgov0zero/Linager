package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FwRule is one rule in nft syntax with its counters.
type FwRule struct {
	Text    string `json:"text"`
	Packets uint64 `json:"packets,omitempty"`
	Bytes   uint64 `json:"bytes,omitempty"`
}

// FwSet is a named list of addresses (an nftables set) that rules can use as "ip saddr @name".
type FwSet struct {
	Name     string   `json:"name"`
	Family   string   `json:"family"` // ipv4 or ipv6
	Elements []string `json:"elements"`
}

var setNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)

// FwChain is a base chain of table inet filter.
type FwChain struct {
	Name   string   `json:"name"`
	Policy string   `json:"policy"`
	Rules  []FwRule `json:"rules"`
}

var (
	chainStartRe = regexp.MustCompile(`^chain\s+(\S+)\s*\{`)
	policyRe     = regexp.MustCompile(`policy\s+(accept|drop)`)
	counterRe    = regexp.MustCompile(`counter packets (\d+) bytes (\d+)`)
	managedNames = []string{"input", "forward", "output"}
)

// parseFilterTable parses the output of `nft list table inet filter`.
func parseFilterTable(out string) []FwChain {
	byName := map[string]*FwChain{}
	var cur *FwChain
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if m := chainStartRe.FindStringSubmatch(line); m != nil {
			cur = &FwChain{Name: m[1], Policy: "accept", Rules: []FwRule{}}
			byName[m[1]] = cur
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case line == "}":
			cur = nil
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "type "):
			if m := policyRe.FindStringSubmatch(line); m != nil {
				cur.Policy = m[1]
			}
		default:
			r := FwRule{Text: line}
			if m := counterRe.FindStringSubmatch(line); m != nil {
				r.Packets, _ = strconv.ParseUint(m[1], 10, 64)
				r.Bytes, _ = strconv.ParseUint(m[2], 10, 64)
				r.Text = counterRe.ReplaceAllString(line, "counter")
			}
			cur.Rules = append(cur.Rules, r)
		}
	}
	out2 := make([]FwChain, 0, 3)
	for _, n := range managedNames {
		if c := byName[n]; c != nil {
			out2 = append(out2, *c)
		} else {
			out2 = append(out2, FwChain{Name: n, Policy: "accept", Rules: []FwRule{}})
		}
	}
	return out2
}

var (
	setStartRe = regexp.MustCompile(`^set\s+(\S+)\s*\{`)
	setTypeRe  = regexp.MustCompile(`^type\s+(ipv4_addr|ipv6_addr)\b`)
)

// parseSets extracts the address sets from the output of `nft list table inet filter`.
func parseSets(out string) []FwSet {
	sets := []FwSet{}
	var cur *FwSet
	inElems := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if cur == nil {
			if m := setStartRe.FindStringSubmatch(line); m != nil {
				cur = &FwSet{Name: m[1], Family: "", Elements: []string{}}
			}
			continue
		}
		if inElems {
			cur.Elements = append(cur.Elements, splitElements(line)...)
			if strings.Contains(line, "}") {
				inElems = false
			}
			continue
		}
		switch {
		case line == "}":
			if cur.Family != "" { // sets of other types (ports, interfaces) are not managed here
				sets = append(sets, *cur)
			}
			cur = nil
		case strings.HasPrefix(line, "elements"):
			_, after, _ := strings.Cut(line, "{")
			cur.Elements = append(cur.Elements, splitElements(after)...)
			inElems = !strings.Contains(line, "}")
		default:
			if m := setTypeRe.FindStringSubmatch(line); m != nil {
				cur.Family = map[string]string{"ipv4_addr": "ipv4", "ipv6_addr": "ipv6"}[m[1]]
			}
		}
	}
	return sets
}

// splitElements takes the addresses out of a piece of an "elements = { ... }" list.
func splitElements(s string) []string {
	s, _, _ = strings.Cut(s, "}")
	var out []string
	for _, part := range strings.Split(s, ",") {
		if f := strings.Fields(part); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// validateSet checks a set of the draft: a name, a family and addresses or networks of that family.
func validateSet(s FwSet) error {
	if !setNameRe.MatchString(s.Name) {
		return fmt.Errorf("list %q: the name must be lowercase letters, digits and _, starting with a letter", s.Name)
	}
	if s.Family != "ipv4" && s.Family != "ipv6" {
		return fmt.Errorf("list %s: unknown address family", s.Name)
	}
	if len(s.Elements) > 5000 {
		return fmt.Errorf("list %s: too many addresses", s.Name)
	}
	for _, e := range s.Elements {
		var a netip.Addr
		if pfx, err := netip.ParsePrefix(e); err == nil {
			a = pfx.Addr()
		} else if ad, err := netip.ParseAddr(e); err == nil {
			a = ad
		} else {
			return fmt.Errorf("list %s: %q is not an address or a network", s.Name, e)
		}
		if (s.Family == "ipv4") != a.Is4() {
			return fmt.Errorf("list %s: %q does not match the family %s", s.Name, e, s.Family)
		}
	}
	return nil
}

// validateRule rejects anything that could escape the chain block.
func validateRule(text string) error {
	t := strings.TrimSpace(text)
	if t == "" || len(t) > 500 {
		return errors.New("rule is empty or too long")
	}
	if strings.ContainsAny(t, "\n\r\x00") {
		return errors.New("rule must be a single line")
	}
	depth := 0
	for _, c := range t {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return errors.New("unbalanced braces")
			}
		}
	}
	if depth != 0 {
		return errors.New("unbalanced braces")
	}
	return nil
}

// fwDraft is the desired state of the managed chains.
type fwDraft struct {
	Sets   []FwSet `json:"sets"`
	Chains []struct {
		Name   string `json:"name"`
		Policy string `json:"policy"`
		Rules  []struct {
			Text string `json:"text"`
		} `json:"rules"`
	} `json:"chains"`
}

// renderTable renders table inet filter for the draft.
func renderTable(d fwDraft) (string, error) {
	var b strings.Builder
	b.WriteString("table inet filter {\n")
	names := map[string]bool{}
	for _, set := range d.Sets {
		if err := validateSet(set); err != nil {
			return "", err
		}
		if names[set.Name] {
			return "", fmt.Errorf("list %s is defined twice", set.Name)
		}
		names[set.Name] = true
		typ := map[string]string{"ipv4": "ipv4_addr", "ipv6": "ipv6_addr"}[set.Family]
		fmt.Fprintf(&b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n", set.Name, typ)
		if len(set.Elements) > 0 {
			fmt.Fprintf(&b, "\t\telements = { %s }\n", strings.Join(set.Elements, ", "))
		}
		b.WriteString("\t}\n")
	}
	seen := map[string]bool{}
	for _, c := range d.Chains {
		ok := false
		for _, n := range managedNames {
			ok = ok || c.Name == n
		}
		if !ok || seen[c.Name] {
			return "", fmt.Errorf("unexpected chain %q", c.Name)
		}
		seen[c.Name] = true
		if c.Policy != "accept" && c.Policy != "drop" {
			return "", fmt.Errorf("chain %s: policy must be accept or drop", c.Name)
		}
		fmt.Fprintf(&b, "\tchain %s {\n\t\ttype filter hook %s priority filter; policy %s;\n", c.Name, c.Name, c.Policy)
		for i, r := range c.Rules {
			if err := validateRule(r.Text); err != nil {
				return "", fmt.Errorf("chain %s rule %d: %w", c.Name, i+1, err)
			}
			fmt.Fprintf(&b, "\t\t%s\n", strings.TrimSpace(r.Text))
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// replaceScript is the atomic nft script replacing table inet filter.
func replaceScript(table string) string {
	return "table inet filter\ndelete table inet filter\n" + table
}

// replaceTableInConf swaps (or appends) `table inet filter { ... }` in an nftables.conf text.
func replaceTableInConf(conf, table string) string {
	re := regexp.MustCompile(`(?m)^[ \t]*table\s+inet\s+filter\s*\{`)
	loc := re.FindStringIndex(conf)
	if loc == nil {
		if strings.TrimSpace(conf) == "" {
			return "#!/usr/sbin/nft -f\n\nflush ruleset\n\n" + table
		}
		return strings.TrimRight(conf, "\n") + "\n\n" + table
	}
	depth, end := 0, -1
	for i := loc[1] - 1; i < len(conf); i++ {
		switch conf[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return conf // malformed: leave the user's file alone
	}
	for end < len(conf) && conf[end] == '\n' {
		end++
	}
	return conf[:loc[0]] + table + conf[end:]
}

type fwModule struct {
	pend     *Pending
	dir      string
	confPath string
	run      cmdRunner
}

func newFwModule(p *Pending, dir string) *fwModule {
	return &fwModule{pend: p, dir: dir, confPath: "/etc/nftables.conf", run: execRunner}
}

func nftStdin(ctx context.Context, script string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "nft", args...) //nolint:gosec // fixed binary, fixed flags
	cmd.Stdin = strings.NewReader(script)
	return cmd.CombinedOutput()
}

func (m *fwModule) currentTable(ctx context.Context) string {
	out, err := m.run(ctx, "nft", "list", "table", "inet", "filter")
	if err != nil {
		return ""
	}
	return string(out)
}

func (m *fwModule) state(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return map[string]any{"available": false, "ufw_active": false, "chains": []FwChain{}, "ruleset": "", "other_tables": []string{}}, nil
	}
	table := m.currentTable(ctx)
	chains := parseFilterTable(table)
	ruleset, _ := m.run(ctx, "nft", "list", "ruleset")
	others := []string{}
	if out, err := m.run(ctx, "nft", "list", "tables"); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "table ") && l != "table inet filter" {
				others = append(others, strings.TrimPrefix(l, "table "))
			}
		}
	}
	ufw := false
	if _, err := exec.LookPath("ufw"); err == nil {
		if out, err := m.run(ctx, "ufw", "status"); err == nil && bytes.Contains(out, []byte("Status: active")) {
			ufw = true
		}
	}
	return map[string]any{"available": true, "ufw_active": ufw, "chains": chains, "sets": parseSets(table), "ruleset": string(ruleset), "other_tables": others,
		"ssh_ports": m.sshPorts(ctx)}, nil
}

var sshPortRe = regexp.MustCompile(`(?mi)^\s*port\s+(\d{1,5})\s*$`)

// parseSSHPorts extracts the listening ports from `sshd -T` output or sshd_config text.
func parseSSHPorts(conf string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, m := range sshPortRe.FindAllStringSubmatch(conf, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 65536 && !seen[n] {
			seen[n] = true
			ports = append(ports, n)
		}
	}
	return ports
}

// sshPorts returns the ports sshd listens on, so the panel can warn before a rule locks the
// administrator out. The effective configuration is preferred, the files are the fallback.
func (m *fwModule) sshPorts(ctx context.Context) []int {
	if out, err := m.run(ctx, "sshd", "-T"); err == nil {
		if p := parseSSHPorts(string(out)); len(p) > 0 {
			return p
		}
	}
	files, _ := filepath.Glob("/etc/ssh/sshd_config.d/*.conf")
	var conf strings.Builder
	for _, f := range append([]string{"/etc/ssh/sshd_config"}, files...) {
		if b, err := os.ReadFile(f); err == nil { //nolint:gosec // fixed sshd configuration paths
			conf.Write(b)
			conf.WriteByte('\n')
		}
	}
	if p := parseSSHPorts(conf.String()); len(p) > 0 {
		return p
	}
	return []int{22}
}

func (m *fwModule) validate(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var d fwDraft
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errors.New("invalid request")
	}
	table, err := renderTable(d)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if out, err := nftStdin(cctx, replaceScript(table), "-c", "-f", "-"); err != nil {
		return map[string]any{"ok": false, "error": cleanNftError(string(out))}, nil
	}
	return map[string]any{"ok": true}, nil
}

func cleanNftError(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if _, after, ok := strings.Cut(l, "Error:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return lastLine(s, "nft rejected the rules")
}

type fwPending struct {
	Restore string `json:"restore"` // script that brings back the previous table
}

func (m *fwModule) apply(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		ID string `json:"id"`
		fwDraft
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" {
		return nil, errors.New("invalid request")
	}
	table, err := renderTable(p.fwDraft)
	if err != nil {
		return nil, err
	}
	script := replaceScript(table)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if out, err := nftStdin(cctx, script, "-c", "-f", "-"); err != nil {
		return nil, errors.New(cleanNftError(string(out)))
	}
	restore := "table inet filter\ndelete table inet filter\n"
	if old := m.currentTable(ctx); old != "" {
		restore += old
	}
	deadline, err := m.pend.Begin(p.ID, "firewall", fwPending{Restore: restore}, PendingWindow)
	if err != nil {
		return nil, err
	}
	if out, err := nftStdin(cctx, script, "-f", "-"); err != nil {
		m.pend.Abort(p.ID)
		return nil, errors.New(cleanNftError(string(out)))
	}
	return map[string]any{"id": p.ID, "deadline": deadline}, nil
}

func (m *fwModule) kind() PendingKind {
	return PendingKind{
		Rollback: func(raw json.RawMessage) error {
			var d fwPending
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if out, err := nftStdin(ctx, d.Restore, "-f", "-"); err != nil {
				return errors.New(cleanNftError(string(out)))
			}
			return nil
		},
		Commit: func(json.RawMessage) error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			table := m.currentTable(ctx)
			if table == "" {
				return errors.New("table inet filter is missing")
			}
			old, _ := os.ReadFile(m.confPath) //nolint:gosec // the nftables configuration file
			if len(old) > 0 {
				_ = os.MkdirAll(m.dir, 0o700)
				_ = os.WriteFile(filepath.Join(m.dir, "nftables.conf.bak-"+time.Now().Format("20060102-150405")), old, 0o600) //nolint:gosec // fixed configuration path
			}
			mode := os.FileMode(0o755) // Debian's default nftables.conf is executable
			if st, err := os.Stat(m.confPath); err == nil {
				mode = st.Mode().Perm()
			}
			tmp := m.confPath + ".linager-new"
			if err := os.WriteFile(tmp, []byte(replaceTableInConf(string(old), table)), mode); err != nil { //nolint:gosec // the mode mirrors the existing file
				return err
			}
			return os.Rename(tmp, m.confPath)
		},
	}
}
