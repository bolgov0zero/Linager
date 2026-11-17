package server

import (
	"os/exec"
	"strings"
	"testing"
)

// TestJoinScript checks the text that `wget ... | sh` runs on a new host: it must be valid shell, take the binary
// with wget (no curl, no sudo), compare its checksum and, for a person at a terminal, open the menu at the end.
func TestJoinScript(t *testing.T) {
	script := joinScriptText("10.9.9.9:9443", "SHA256:fp", "sha256//pin=", "lk_AAAAAAAAAAAAAAAAAAAAAA", map[string]string{"amd64": strings.Repeat("ab", 32), "arm64": strings.Repeat("cd", 32)})
	for _, must := range []string{
		"wget -nv --no-check-certificate --pinnedpubkey",
		`SERVER='10.9.9.9:9443'`,
		"amd64) want='" + strings.Repeat("ab", 32) + "' ;;",
		"install agent --no-tui",
		"--join-token",
		"read -r _ </dev/tty",
		"exec /usr/local/bin/linager </dev/tty", // the menu, as if the person typed linager
		"rm -rf \"$tmp\"\ntrap - EXIT INT TERM", // the downloaded copy goes before exec skips the trap
	} {
		if !strings.Contains(script, must) {
			t.Errorf("the script lacks %q", must)
		}
	}
	for _, bad := range []string{"curl", "sudo"} {
		if strings.Contains(script, bad) {
			t.Errorf("the script must not use %q", bad)
		}
	}
	// a hostile value cannot leave its quotes
	evil := joinScriptText("x';id;'", "fp", "pin", "t", nil)
	if strings.Contains(evil, "x';id;'\n") || !strings.Contains(evil, `'x'\'';id;'\'''`) {
		t.Errorf("a value was not quoted:\n%s", evil)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to check the syntax")
	}
	for name, text := range map[string]string{"normal": script, "no checksums": joinScriptText("h:1", "fp", "pin", "t", nil)} {
		cmd := exec.Command(sh, "-n") //nolint:gosec // sh only parses the generated script, it does not run it
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: not valid shell: %v\n%s", name, err, out)
		}
	}
}
