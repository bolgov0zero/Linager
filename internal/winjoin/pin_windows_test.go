//go:build windows

package winjoin

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runPin runs the pin code in Windows PowerShell against url with the fingerprint fp and returns what it printed.
func runPin(t *testing.T, fp, url string) (string, error) {
	t.Helper()
	script := PinSetup(fp) + "; (New-Object Net.WebClient).DownloadString(" + psq(url) + ")"
	root := os.Getenv("SystemRoot")
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	var env []string
	for _, e := range os.Environ() { // a PowerShell 7 parent hands down a module path that Windows PowerShell cannot use
		if !strings.HasPrefix(strings.ToUpper(e), "PSMODULEPATH=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// The pin of the install command must accept the certificate of the server with that fingerprint and refuse any other.
func TestPowerShellPinAcceptsOnlyTheRightCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello-from-the-panel")) }))
	defer srv.Close()
	sum := sha256.Sum256(srv.Certificate().Raw)
	good := "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
	out, err := runPin(t, good, srv.URL)
	if err != nil || !strings.Contains(out, "hello-from-the-panel") {
		t.Fatalf("the right fingerprint must be accepted: %v %q", err, out)
	}
	out, err = runPin(t, "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", srv.URL)
	if err == nil || strings.Contains(out, "hello-from-the-panel") {
		t.Fatalf("another fingerprint must be refused: %v %q", err, out)
	}
}
