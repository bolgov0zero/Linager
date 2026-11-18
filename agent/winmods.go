package agent

// Logic of the Windows users, updates and network modules that does not call Windows, tested on every platform.

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// winUserRe is a local account name: Windows allows up to 20 characters and forbids a few.
var winUserRe = regexp.MustCompile(`^[^\\/\[\]:;|=,+*?<>"@\x00-\x1f ][^\\/\[\]:;|=,+*?<>"@\x00-\x1f]{0,19}$`)

// winGroupRe is a local group name (up to 256 characters, same forbidden set).
var winGroupRe = regexp.MustCompile(`^[^\\/\[\]:;|=,+*?<>"@\x00-\x1f][^\\/\[\]:;|=,+*?<>"@\x00-\x1f]{0,255}$`)

func validWinUser(name string) bool {
	return winUserRe.MatchString(name) && !strings.HasSuffix(name, ".")
}

func validWinGroups(groups []string) error {
	if len(groups) > 50 {
		return errors.New("too many groups")
	}
	for _, g := range groups {
		if !winGroupRe.MatchString(g) {
			return fmt.Errorf("invalid group name %q", g)
		}
	}
	return nil
}

// builtinAdminSID tells the built-in administrator account (its SID ends in -500, whatever its name).
func builtinAdminSID(sid string) bool {
	return strings.HasPrefix(sid, "S-1-5-21-") && strings.HasSuffix(sid, "-500")
}

// netApply is the request "network.apply" of the Windows agent.
type netApply struct {
	Index   int      `json:"index"`
	Mode    string   `json:"mode"` // dhcp | static
	IP      string   `json:"ip"`
	Prefix  int      `json:"prefix"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
}

// validateNetApply checks a network change before anything is touched. The DNS list of a static change may be empty.
func validateNetApply(r *netApply) error {
	if r.Index <= 0 {
		return errors.New("invalid adapter")
	}
	switch r.Mode {
	case "dhcp":
		r.IP, r.Gateway, r.Prefix = "", "", 0
	case "static":
		ip := net.ParseIP(r.IP).To4()
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
			return errors.New("invalid IPv4 address")
		}
		if r.Prefix < 1 || r.Prefix > 30 {
			return errors.New("the prefix length must be from 1 to 30")
		}
		if r.Gateway != "" {
			gw := net.ParseIP(r.Gateway).To4()
			if gw == nil || gw.Equal(ip) {
				return errors.New("invalid gateway")
			}
			_, n, _ := net.ParseCIDR(fmt.Sprintf("%s/%d", ip, r.Prefix))
			if !n.Contains(gw) {
				return errors.New("the gateway is not in the network of the address")
			}
		}
	default:
		return errors.New("invalid mode")
	}
	if len(r.DNS) > 4 {
		return errors.New("at most 4 DNS servers")
	}
	for _, d := range r.DNS {
		if p := net.ParseIP(d).To4(); p == nil || p.IsUnspecified() {
			return fmt.Errorf("invalid DNS server %q", d)
		}
	}
	return nil
}
