package agent

// Logic of the Windows agent that does not call Windows, so that it is tested on every platform.

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// normalizeWinPath checks a path of the panel ("/C:/Users/x", "C:\\Users\\x", "C:") and returns it as "C:/Users/x".
func normalizeWinPath(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", errors.New("invalid path")
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 3 && p[0] == '/' && isDriveLetter(p[1]) && p[2] == ':' {
		p = p[1:] // "/C:/x" is how the panel names it
	}
	if len(p) == 2 && p[1] == ':' {
		p += "/"
	}
	if len(p) < 3 || !isDriveLetter(p[0]) || p[1] != ':' || p[2] != '/' {
		return "", errors.New("invalid path")
	}
	return strings.ToUpper(p[:1]) + ":" + path.Clean(p[2:]), nil
}

func isDriveLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// windowsName builds the name shown in the panel. Windows 11 still reports "Windows 10" as the product name:
// the build number (22000 and up) tells them apart.
func windowsName(product, display, build string) string {
	if product == "" {
		product = "Windows"
	}
	if n, err := strconv.Atoi(build); err == nil && n >= 22000 && len(product) >= 10 && product[:10] == "Windows 10" {
		product = "Windows 11" + product[10:]
	}
	switch {
	case display != "" && build != "":
		return fmt.Sprintf("%s (%s, build %s)", product, display, build)
	case build != "":
		return fmt.Sprintf("%s (build %s)", product, build)
	}
	return product
}

var logNameRe = regexp.MustCompile(`^[A-Za-z0-9 _.*/-]{1,100}$`)

// eventParams are the parameters of the "logs" stream sent by the panel.
type eventParams struct {
	Unit     string `json:"unit"`
	Priority string `json:"priority"`
	Kernel   string `json:"kernel"`
	Lines    int    `json:"lines"`
}

// standardLogs are the classic event logs: a "unit" with one of these names selects the log, any other name is
// taken for the provider (the source) of the events, searched in System and Application.
var standardLogs = map[string]string{
	"system": "System", "application": "Application", "security": "Security", "setup": "Setup",
	"forwardedevents": "ForwardedEvents", "windows powershell": "Windows PowerShell",
}

// eventQuery turns the parameters of the panel into the log names, the provider and the highest Windows level
// (1 critical, 2 error, 3 warning, 4 information; 0 = all) of the events to show.
func eventQuery(p eventParams) (logs []string, provider string, maxLevel int) {
	logs = []string{"System", "Application"}
	if p.Kernel == "1" {
		logs, provider = []string{"System"}, "Microsoft-Windows-Kernel-*"
	} else if u := strings.TrimSpace(p.Unit); u != "" && logNameRe.MatchString(u) {
		if l, ok := standardLogs[strings.ToLower(u)]; ok {
			logs = []string{l}
		} else if strings.Contains(u, "/") {
			logs = []string{u} // an operational log such as Microsoft-Windows-TaskScheduler/Operational
		} else {
			provider = u
		}
	}
	// syslog priority (0 emergency .. 7 debug) to the Windows level
	if v, err := strconv.Atoi(p.Priority); err == nil {
		switch {
		case v >= 0 && v <= 2:
			maxLevel = 1
		case v == 3:
			maxLevel = 2
		case v == 4:
			maxLevel = 3
		case v == 5 || v == 6:
			maxLevel = 4
		}
	}
	return logs, provider, maxLevel
}

// encodePowerShell is the form powershell.exe -EncodedCommand takes: base64 of the UTF-16 text.
func encodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return base64.StdEncoding.EncodeToString(b)
}
