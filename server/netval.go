package server

import (
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// lang picks the message language from the browser's Accept-Language header.
func lang(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ru") {
		return "ru"
	}
	return "en"
}

func msg(r *http.Request, ru, en string) string {
	if lang(r) == "ru" {
		return ru
	}
	return en
}

// NetChange is one edited interface of a network draft.
type NetChange struct {
	Iface   string `json:"iface"`
	Mode    string `json:"mode"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	MTU     int    `json:"mtu"`
	Up      bool   `json:"up"`
}

// FieldError is a validation problem of one field.
type FieldError struct {
	Iface   string `json:"iface"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type netIfaceInfo struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	Editable  bool     `json:"editable"`
	Primary   bool     `json:"primary"`
	Kind      string   `json:"kind"`
}

// validateNetDraft checks the draft against the current interfaces. text yields localized messages.
func validateNetDraft(changes []NetChange, ifaces []netIfaceInfo, text func(ru, en string) string) []FieldError {
	var errs []FieldError
	add := func(i, f, m string) { errs = append(errs, FieldError{Iface: i, Field: f, Message: m}) }
	byName := map[string]netIfaceInfo{}
	for _, i := range ifaces {
		byName[i.Name] = i
	}
	// subnets of every interface after the draft is applied
	type net4 struct {
		iface string
		n     *net.IPNet
	}
	var nets []net4
	changed := map[string]NetChange{}
	for _, c := range changes {
		changed[c.Iface] = c
	}
	for _, i := range ifaces {
		if i.Kind == "loopback" {
			continue
		}
		if c, ok := changed[i.Name]; ok {
			if c.Mode == "static" {
				if _, n, err := net.ParseCIDR(c.Address); err == nil {
					nets = append(nets, net4{i.Name, n})
				}
			}
			continue
		}
		for _, a := range i.Addresses {
			if _, n, err := net.ParseCIDR(a); err == nil {
				nets = append(nets, net4{i.Name, n})
			}
		}
	}
	seen := map[string]bool{}
	for _, c := range changes {
		cur, ok := byName[c.Iface]
		switch {
		case seen[c.Iface]:
			add(c.Iface, "iface", text("Интерфейс указан дважды", "Interface listed twice"))
			continue
		case !ok:
			add(c.Iface, "iface", text("Интерфейс не найден", "Interface not found"))
			continue
		case !cur.Editable:
			add(c.Iface, "iface", text("Этот интерфейс нельзя менять", "This interface cannot be edited"))
			continue
		}
		seen[c.Iface] = true
		if c.Mode != "static" && c.Mode != "dhcp" {
			add(c.Iface, "mode", text("Режим: static или dhcp", "Mode must be static or dhcp"))
		}
		if c.MTU != 0 && (c.MTU < 68 || c.MTU > 9000) {
			add(c.Iface, "mtu", text("MTU от 68 до 9000", "MTU must be between 68 and 9000"))
		}
		if c.Mode != "static" {
			continue
		}
		ip, nw, err := net.ParseCIDR(strings.TrimSpace(c.Address))
		if err != nil || ip.To4() == nil {
			add(c.Iface, "address", text("Адрес IPv4 в виде 10.0.0.5/24", "IPv4 address like 10.0.0.5/24"))
			continue
		}
		if g := strings.TrimSpace(c.Gateway); g != "" {
			gw := net.ParseIP(g)
			switch {
			case gw == nil || gw.To4() == nil:
				add(c.Iface, "gateway", text("Некорректный адрес шлюза", "Invalid gateway address"))
			case !nw.Contains(gw):
				add(c.Iface, "gateway", text("Шлюз вне подсети интерфейса", "Gateway is outside the interface subnet"))
			case gw.Equal(ip):
				add(c.Iface, "gateway", text("Шлюз совпадает с адресом интерфейса", "Gateway equals the interface address"))
			}
		}
		for _, o := range nets {
			if o.iface != c.Iface && (o.n.Contains(nw.IP) || nw.Contains(o.n.IP)) {
				add(c.Iface, "address", text(fmt.Sprintf("Подсеть пересекается с %s (%s)", o.iface, o.n), fmt.Sprintf("Subnet overlaps with %s (%s)", o.iface, o.n)))
				break
			}
		}
	}
	return errs
}

// Warning is a risk shown before applying a change.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// netWarnings lists the risks of applying the network draft.
func netWarnings(changes []NetChange, ifaces []netIfaceInfo, text func(ru, en string) string) []Warning {
	var w []Warning
	for _, c := range changes {
		for _, i := range ifaces {
			if i.Name != c.Iface || !i.Primary {
				continue
			}
			w = append(w, Warning{"primary_iface", text("Меняется основной интерфейс "+i.Name+": связь с агентом может прерваться", "The primary interface "+i.Name+" changes: the agent connection may drop")})
			if !c.Up {
				w = append(w, Warning{"agent_link", text("Основной интерфейс будет отключён: подтвердить нельзя, настройки откатятся сами через 30 секунд", "The primary interface will be disabled: you cannot confirm, the settings roll back by themselves after 30 seconds")})
			}
		}
	}
	return w
}

var (
	dportRe      = regexp.MustCompile(`(?i)\b(udp\s+)?dport\s+(\{[^}]*\}|[\w-]+)`)
	verdictRe    = regexp.MustCompile(`(?i)\b(accept|drop|reject)\b`)
	establishedR = regexp.MustCompile(`(?i)\bct\s+state\b[^#]*\bestablished\b[^#]*\baccept\b|\baccept\b[^#]*\bct\s+state\b[^#]*\bestablished\b`)
)

// ruleVerdictFor reports whether a rule matches TCP destination port `port` and with which verdict
// ("accept", "drop", "reject", or "" when the rule only counts or logs).
func ruleVerdictFor(rule string, port int) (matches bool, verdict string) {
	if i := strings.IndexByte(rule, '#'); i >= 0 {
		rule = rule[:i]
	}
	m := dportRe.FindStringSubmatch(rule)
	if m == nil || m[1] != "" { // no port match, or a UDP rule
		return false, ""
	}
	hit := false
	for _, item := range strings.Split(strings.Trim(m[2], "{} "), ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		lo, hi, isRange := strings.Cut(item, "-")
		switch {
		case item == "ssh":
			hit = hit || port == 22
		case isRange:
			a, err1 := strconv.Atoi(strings.TrimSpace(lo))
			b, err2 := strconv.Atoi(strings.TrimSpace(hi))
			hit = hit || (err1 == nil && err2 == nil && a <= port && port <= b)
		default:
			n, err := strconv.Atoi(item)
			hit = hit || (err == nil && n == port)
		}
	}
	if !hit {
		return false, ""
	}
	return true, strings.ToLower(verdictRe.FindString(rule))
}

// FwDraftSet is a named list of addresses of a firewall draft; the agent validates the content.
type FwDraftSet struct {
	Name     string   `json:"name"`
	Family   string   `json:"family"`
	Elements []string `json:"elements"`
}

// FwDraftChain is one chain of a firewall draft.
type FwDraftChain struct {
	Name   string `json:"name"`
	Policy string `json:"policy"`
	Rules  []struct {
		Text string `json:"text"`
	} `json:"rules"`
}

// fwWarnings lists the risks of applying the firewall draft. sshPorts are the ports sshd really
// listens on (reported by the agent); an empty list means the default port 22.
func fwWarnings(chains []FwDraftChain, sshPorts []int, text func(ru, en string) string) []Warning {
	if len(sshPorts) == 0 {
		sshPorts = []int{22}
	}
	var w []Warning
	for _, c := range chains {
		switch c.Name {
		case "input":
			for _, port := range sshPorts {
				open := c.Policy == "accept"
				for _, r := range c.Rules { // the first rule with a verdict for the port decides
					if hit, v := ruleVerdictFor(r.Text, port); hit && v != "" {
						open = v == "accept"
						break
					}
				}
				if open {
					continue
				}
				p := strconv.Itoa(port)
				if c.Policy == "drop" {
					w = append(w, Warning{"ssh_port", text("Правила закрывают порт "+p+" (SSH): доступ по SSH может пропасть", "The rules close port "+p+" (SSH): SSH access may be lost")})
				} else {
					w = append(w, Warning{"ssh_port", text("Правила блокируют порт "+p+" (SSH)", "A rule blocks port "+p+" (SSH)")})
				}
			}
			if c.Policy == "drop" {
				est := false
				for _, r := range c.Rules {
					est = est || establishedR.MatchString(r.Text)
				}
				if !est {
					w = append(w, Warning{"agent_link", text("Нет правила для established,related: ответы на исходящие соединения будут отброшены, агент потеряет связь", "No established,related rule: replies to outgoing connections are dropped and the agent loses its link")})
				}
			}
		case "output":
			if c.Policy == "drop" {
				w = append(w, Warning{"agent_link", text("Политика output drop может оборвать исходящее соединение агента", "Output policy drop may cut the agent's outgoing connection")})
			}
		}
	}
	return w
}
