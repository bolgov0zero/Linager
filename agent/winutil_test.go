package agent

import (
	"encoding/base64"
	"slices"
	"testing"
	"unicode/utf16"
)

func TestNormalizeWinPath(t *testing.T) {
	ok := map[string]string{
		"/C:/Users/x":         "C:/Users/x",
		"C:/Users/x":          "C:/Users/x",
		`C:\Users\x`:          "C:/Users/x",
		"c:":                  "C:/",
		"/c:":                 "C:/",
		"C:/":                 "C:/",
		"/D:/a/../b/./c":      "D:/b/c",
		"C:/Program Files/x/": "C:/Program Files/x",
	}
	for in, want := range ok {
		got, err := normalizeWinPath(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "/", "relative/path", "C", "1:/x", `\\server\share`, "C:\x00", "/etc/passwd", "C:x"} {
		if got, err := normalizeWinPath(in); err == nil {
			t.Errorf("%q must be refused, got %q", in, got)
		}
	}
	// a path cannot climb out of its drive
	if got, _ := normalizeWinPath("C:/../../x"); got != "C:/x" {
		t.Errorf("got %q", got)
	}
}

func TestWindowsName(t *testing.T) {
	cases := []struct{ product, display, build, want string }{
		{"Windows Server 2022 Standard", "21H2", "20348", "Windows Server 2022 Standard (21H2, build 20348)"},
		{"Windows 10 Pro", "22H2", "19045", "Windows 10 Pro (22H2, build 19045)"},
		{"Windows 10 Pro", "23H2", "22631", "Windows 11 Pro (23H2, build 22631)"}, // the registry still says 10
		{"Windows 10 Enterprise", "", "22000", "Windows 11 Enterprise (build 22000)"},
		{"", "", "", "Windows"},
	}
	for _, c := range cases {
		if got := windowsName(c.product, c.display, c.build); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}

func TestEventQuery(t *testing.T) {
	logs, prov, lvl := eventQuery(eventParams{})
	if !slices.Equal(logs, []string{"System", "Application"}) || prov != "" || lvl != 0 {
		t.Errorf("default: %v %q %d", logs, prov, lvl)
	}
	if logs, _, _ := eventQuery(eventParams{Unit: "security"}); !slices.Equal(logs, []string{"Security"}) {
		t.Errorf("a classic log: %v", logs)
	}
	if logs, _, _ := eventQuery(eventParams{Unit: "Microsoft-Windows-TaskScheduler/Operational"}); !slices.Equal(logs, []string{"Microsoft-Windows-TaskScheduler/Operational"}) {
		t.Errorf("an operational log: %v", logs)
	}
	if logs, prov, _ := eventQuery(eventParams{Unit: "Service Control Manager"}); !slices.Equal(logs, []string{"System", "Application"}) || prov != "Service Control Manager" {
		t.Errorf("a provider: %v %q", logs, prov)
	}
	if logs, prov, _ := eventQuery(eventParams{Kernel: "1", Unit: "ignored"}); !slices.Equal(logs, []string{"System"}) || prov != "Microsoft-Windows-Kernel-*" {
		t.Errorf("kernel: %v %q", logs, prov)
	}
	for pri, want := range map[string]int{"0": 1, "2": 1, "3": 2, "4": 3, "5": 4, "6": 4, "7": 0, "": 0, "x": 0} {
		if _, _, l := eventQuery(eventParams{Priority: pri}); l != want {
			t.Errorf("priority %q: level %d, want %d", pri, l, want)
		}
	}
	// a name with quotes or a pipe never reaches PowerShell as a log or provider
	if logs, prov, _ := eventQuery(eventParams{Unit: `x'; Remove-Item *; '`}); prov != "" || !slices.Equal(logs, []string{"System", "Application"}) {
		t.Errorf("a hostile name was accepted: %v %q", logs, prov)
	}
}

func TestEncodePowerShell(t *testing.T) {
	enc := encodePowerShell("Write-Output 'é'")
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("bad encoding: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if got := string(utf16.Decode(u)); got != "Write-Output 'é'" {
		t.Errorf("got %q", got)
	}
}

func TestIsDangerousWinService(t *testing.T) {
	for _, c := range []struct {
		name, action string
		want         bool
	}{
		{"LinagerAgent", "stop", true}, {"linageragent", "restart", true}, {"TermService", "disable", true},
		{"LinagerAgent", "start", false}, {"Spooler", "stop", false}, {"WinRM", "enable", false},
	} {
		if got := IsDangerousWinService(c.name, c.action); got != c.want {
			t.Errorf("%s %s: %v", c.name, c.action, got)
		}
	}
}
