package agent

import (
	"net"
	"os"
)

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
