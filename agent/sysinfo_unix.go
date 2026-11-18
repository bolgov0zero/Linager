//go:build !windows

package agent

import (
	"bufio"
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
