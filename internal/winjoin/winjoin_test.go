package winjoin

import (
	"strings"
	"testing"
)

func TestInstallCommandCarriesTheAddressAndThePin(t *testing.T) {
	c := InstallCommand("panel.example.com", "9443", "SHA256:abc", "lk_secret")
	for _, want := range []string{"https://panel.example.com:9443/v1/join/lk_secret/windows", "'SHA256:abc'", "LinagerPin", "ServerCertificateValidationCallback", "iex", "finally"} {
		if !strings.Contains(c, want) {
			t.Errorf("the command lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "\n") {
		t.Error("the command must be one line: it is pasted into a prompt")
	}
	if v6 := InstallCommand("fd00::1", "9443", "SHA256:abc", "lk_s"); !strings.Contains(v6, "https://[fd00::1]:9443/") {
		t.Errorf("an IPv6 address needs brackets: %s", v6)
	}
}

func TestScriptHasEverythingItNeeds(t *testing.T) {
	s := ScriptText("panel:9443", "SHA256:agent", "SHA256:pin", "lk_secret", "deadbeef")
	for _, want := range []string{"$server = 'panel:9443'", "$fp = 'SHA256:agent'", "[LinagerPin]::Fp='SHA256:pin'", "ServerCertificateValidationCallback = $null", "$token = 'lk_secret'", "$want = 'DEADBEEF'", "FINGERPRINT=' + $fp", "join.token", "S-1-5-18", "S-1-5-32-544", "msiexec.exe"} {
		if !strings.Contains(s, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	// a quote in a value cannot end the string
	q := ScriptText("a'b:1", "SHA256:x", "SHA256:y", "t'k", "ab")
	if !strings.Contains(q, "$server = 'a''b:1'") || !strings.Contains(q, "$token = 't''k'") {
		t.Error("single quotes must be doubled")
	}
}
