package installer

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// detectArch runs detect_arch from install.sh with a fake uname and dpkg ("" = no dpkg on the machine).
func detectArch(t *testing.T, uname, dpkgArch string) (string, int) {
	t.Helper()
	src, err := os.ReadFile("../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?ms)^detect_arch\(\) \{.*?^\}$`).Find(src)
	if fn == nil {
		t.Fatal("detect_arch not found in install.sh")
	}
	dir := t.TempDir()
	stub := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { //nolint:gosec // test stub
			t.Fatal(err)
		}
	}
	stub("uname", `echo `+uname)
	if dpkgArch != "" {
		stub("dpkg", `echo `+dpkgArch)
	}
	// only the stubs are on the PATH, so a real dpkg of the machine running the test never shows up
	cmd := exec.Command("/bin/sh", "-c", string(fn)+"\ndetect_arch") //nolint:gosec // the script text comes from the repository's install.sh
	cmd.Env = []string{"PATH=" + dir}
	out, err := cmd.Output()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out)), code
}

func TestInstallScriptDetectsArchitecture(t *testing.T) {
	cases := []struct {
		name, uname, dpkg string
		want              string
		code              int
	}{
		{"amd64", "x86_64", "amd64", "amd64", 0},
		{"amd64 without dpkg", "x86_64", "", "amd64", 0},
		{"Raspberry Pi 4, 64-bit system", "aarch64", "arm64", "arm64", 0},
		{"arm64 without dpkg", "aarch64", "", "arm64", 0},
		{"64-bit kernel with a 32-bit system", "aarch64", "armhf", "", 2},
		{"32-bit kernel", "armv7l", "armhf", "", 1},
		{"32-bit x86", "i686", "i386", "", 1},
		{"riscv", "riscv64", "riscv64", "", 1},
		{"amd64 kernel with an i386 system", "x86_64", "i386", "", 2},
	}
	for _, c := range cases {
		got, code := detectArch(t, c.uname, c.dpkg)
		if got != c.want || code != c.code {
			t.Errorf("%s: got %q (exit %d), want %q (exit %d)", c.name, got, code, c.want, c.code)
		}
	}
}

func TestInstallScriptPicksTheBinaryByArchitecture(t *testing.T) {
	src, err := os.ReadFile("../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, bad := range []string{"linager_linux_amd64", "only amd64"} {
		if strings.Contains(s, bad) {
			t.Errorf("install.sh must not hardcode %q", bad)
		}
	}
}

func TestRaspberryPiOSIsSupported(t *testing.T) {
	// Raspberry Pi OS 64-bit is plain Debian (ID=debian); the 32-bit one says ID=raspbian and is not supported
	pi64 := "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nNAME=\"Debian GNU/Linux\"\nVERSION_ID=\"12\"\nID=debian\nID_LIKE=\n"
	pi32 := "PRETTY_NAME=\"Raspbian GNU/Linux 12 (bookworm)\"\nVERSION_ID=\"12\"\nID=raspbian\nID_LIKE=debian\n"
	if !ParseOSRelease(pi64).Supported() {
		t.Error("Raspberry Pi OS 64-bit (Debian 12) must be supported")
	}
	if ParseOSRelease(pi32).Supported() {
		t.Error("Raspbian (32-bit) must not be supported")
	}
}
