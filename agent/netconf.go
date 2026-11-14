package agent

import (
	"fmt"
	"sort"
	"strings"
)

// NetChange is one edited interface (see the panel's network draft).
type NetChange struct {
	Iface   string `json:"iface"`
	Mode    string `json:"mode"` // static | dhcp
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	MTU     int    `json:"mtu"`
	Up      bool   `json:"up"`
}

// ifaceBlock is an `iface <name> inet <method>` stanza with its option lines.
type ifaceBlock struct {
	name, family, method string
	start, end           int // line range [start, end)
	opts                 []string
}

var stanzaKeywords = map[string]bool{"auto": true, "allow-auto": true, "allow-hotplug": true, "iface": true, "mapping": true,
	"source": true, "source-directory": true, "no-auto-down": true, "no-scripts": true}

func firstWord(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return ""
	}
	return f[0]
}

// parseIfaceBlocks finds the iface stanzas of an interfaces(5) file.
func parseIfaceBlocks(lines []string) []*ifaceBlock {
	var out []*ifaceBlock
	for i := 0; i < len(lines); i++ {
		f := strings.Fields(lines[i])
		if len(f) < 4 || f[0] != "iface" {
			continue
		}
		b := &ifaceBlock{name: f[1], family: f[2], method: f[3], start: i}
		j := i + 1
		for ; j < len(lines); j++ {
			if w := firstWord(lines[j]); stanzaKeywords[w] {
				break
			}
			if strings.TrimSpace(lines[j]) != "" && !strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
				b.opts = append(b.opts, lines[j])
			}
		}
		// trailing blank/comment lines belong to the gap between stanzas, not the block
		end := j
		for end > i+1 && (strings.TrimSpace(lines[end-1]) == "" || strings.HasPrefix(strings.TrimSpace(lines[end-1]), "#")) {
			end--
		}
		b.end = end
		out = append(out, b)
		i = j - 1
	}
	return out
}

var managedOpts = map[string]bool{"address": true, "netmask": true, "gateway": true, "broadcast": true, "network": true, "mtu": true}

func renderBlock(c NetChange, old *ifaceBlock) []string {
	method := "dhcp"
	if c.Mode == "static" {
		method = "static"
	}
	out := []string{fmt.Sprintf("iface %s inet %s", c.Iface, method)}
	if method == "static" {
		out = append(out, "    address "+c.Address)
		if c.Gateway != "" {
			out = append(out, "    gateway "+c.Gateway)
		}
	}
	if c.MTU > 0 && c.MTU != 1500 {
		out = append(out, fmt.Sprintf("    mtu %d", c.MTU))
	}
	if old != nil {
		for _, o := range old.opts {
			if !managedOpts[firstWord(o)] {
				out = append(out, o)
			}
		}
	}
	return out
}

// setAuto adds or removes name from the `auto`/`allow-hotplug` lines.
func setAuto(lines []string, name string, up bool) []string {
	found := false
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[0] == "auto" || f[0] == "allow-hotplug" || f[0] == "allow-auto") {
			var names []string
			has := false
			for _, n := range f[1:] {
				if n == name {
					has = true
					continue
				}
				names = append(names, n)
			}
			if has {
				found = true
				if up {
					out = append(out, l)
					continue
				}
				if len(names) > 0 {
					out = append(out, f[0]+" "+strings.Join(names, " "))
				}
				continue
			}
		}
		out = append(out, l)
	}
	if up && !found {
		// insert before the iface stanza of this interface when present, else append
		for i, l := range out {
			f := strings.Fields(l)
			if len(f) >= 3 && f[0] == "iface" && f[1] == name {
				res := append([]string{}, out[:i]...)
				res = append(res, "auto "+name)
				return append(res, out[i:]...)
			}
		}
		return append(out, "auto "+name)
	}
	return out
}

// applyNetChanges returns the interfaces file text with the changes applied. Only `inet` stanzas of the
// given file are edited; other options of a stanza (dns-*, hooks, hwaddress) are preserved.
func applyNetChanges(text string, changes []NetChange) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, c := range changes {
		var blk *ifaceBlock
		for _, b := range parseIfaceBlocks(lines) {
			if b.name == c.Iface && b.family == "inet" {
				blk = b
				break
			}
		}
		nb := renderBlock(c, blk)
		if blk == nil {
			if lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			lines = append(lines, nb...)
		} else {
			res := append([]string{}, lines[:blk.start]...)
			res = append(res, nb...)
			lines = append(res, lines[blk.end:]...)
		}
		lines = setAuto(lines, c.Iface, c.Up)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// maxRawConfig caps a hand-edited interfaces file.
const maxRawConfig = 64 * 1024

// autoNames returns the interface names listed in auto / allow-hotplug / allow-auto lines.
func autoNames(text string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[0] == "auto" || f[0] == "allow-hotplug" || f[0] == "allow-auto") {
			for _, n := range f[1:] {
				out[n] = true
			}
		}
	}
	return out
}

// stanzaTexts maps "<name> <family>" to the text of its iface stanza.
func stanzaTexts(text string) map[string]string {
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	for _, b := range parseIfaceBlocks(lines) {
		out[b.name+" "+b.family] = strings.Join(lines[b.start:b.end], "\n")
	}
	return out
}

// diffIfaces lists the interfaces (never lo) whose stanza or auto membership differs between two interfaces
// files. up tells whether the interface has a stanza in the new text and should be brought up afterwards
// (it is auto, or it is up right now).
func diffIfaces(oldText, newText string, isUp func(string) bool) (names []string, up []bool) {
	o, n := stanzaTexts(oldText), stanzaTexts(newText)
	oa, na := autoNames(oldText), autoNames(newText)
	seen := map[string]bool{}
	keys := make([]string, 0, len(o)+len(n))
	for k := range o {
		keys = append(keys, k)
	}
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name := strings.Fields(k)[0]
		if name == "lo" || seen[name] {
			continue
		}
		if o[k] == n[k] && oa[name] == na[name] {
			continue
		}
		seen[name] = true
		hasNew := false
		for nk := range n {
			if strings.Fields(nk)[0] == name {
				hasNew = true
			}
		}
		names = append(names, name)
		up = append(up, hasNew && (na[name] || isUp(name)))
	}
	return names, up
}
