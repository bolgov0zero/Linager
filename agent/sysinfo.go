package agent

import (
	"bufio"
	"net"
	"os"
	"strings"
)

// osName returns PRETTY_NAME from /etc/os-release.
func osName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "linux"
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "linux"
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// localIP is the address used to reach the server.
func localIP(c net.Conn) string {
	if a, ok := c.LocalAddr().(*net.TCPAddr); ok {
		return a.IP.String()
	}
	return ""
}
