package server

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bolgov0zero/linager/internal/config"
)

var specRe = regexp.MustCompile(`^(any|admin|srv|srv:admin|srv:(services|apt|disk|security|processes|files|network|firewall|docker)|mod:terminal)$`)

// Every authenticated route has an access rule, and the loose ones ("any", "srv") are only the ones that are
// meant to be: a new route cannot slip in open to an operator or an auditor by accident.
func TestRouteTableIsComplete(t *testing.T) {
	a := &Admin{Cfg: &config.Server{}}
	_ = a.Handler()
	if len(a.routes) < 100 {
		t.Fatalf("the route table looks empty: %d routes", len(a.routes))
	}
	anyOK := map[string]bool{
		"POST /api/logout": true, "GET /api/me": true, "POST /api/me/totp/setup": true, "POST /api/me/totp/enable": true,
		"POST /api/me/totp/disable": true, "POST /api/me/password": true, "POST /api/me/recovery-codes": true,
		"GET /api/me/passkeys": true, "POST /api/me/passkeys/begin": true, "POST /api/me/passkeys/finish": true,
		"DELETE /api/me/passkeys/{id}": true, "GET /api/servers": true, "GET /api/groups": true, "GET /api/dashboard": true,
		"PUT /api/dashboard/order": true, "GET /api/live": true,
	}
	srvOK := map[string]bool{
		"GET /api/servers/{id}": true, "GET /api/servers/{id}/metrics": true, "GET /api/servers/{id}/metrics.csv": true,
		"GET /api/servers/{id}/stream": true, // the module is checked by the kind of the stream
	}
	seen := map[string]bool{}
	for _, r := range a.routes {
		if seen[r.Pattern] {
			t.Errorf("route %s is registered twice", r.Pattern)
		}
		seen[r.Pattern] = true
		if !specRe.MatchString(r.Spec) {
			t.Errorf("route %s has an unknown rule %q", r.Pattern, r.Spec)
		}
		path := r.Pattern[strings.IndexByte(r.Pattern, ' ')+1:]
		switch {
		case r.Spec == "any" && !anyOK[r.Pattern]:
			t.Errorf("route %s is open to every role: add it to the allowed list on purpose", r.Pattern)
		case r.Spec == "srv" && !srvOK[r.Pattern]:
			t.Errorf("route %s is open to auditors: add it to the allowed list on purpose", r.Pattern)
		case strings.HasPrefix(path, "/api/servers/{id}") && !strings.HasPrefix(r.Spec, "srv"):
			t.Errorf("route %s names a server but its rule %q does not check access to it", r.Pattern, r.Spec)
		}
	}
	for _, must := range []string{"DELETE /api/servers/{id}", "POST /api/servers/{id}/reboot", "POST /api/enrollments/{id}/approve", "GET /api/audit", "POST /api/users", "PUT /api/users/{id}/access", "POST /api/updates/agents"} {
		var spec string
		for _, r := range a.routes {
			if r.Pattern == must {
				spec = r.Spec
			}
		}
		if spec != "admin" && spec != "srv:admin" {
			t.Errorf("%s must be for administrators only, it has %q", must, spec)
		}
	}
}

func TestStreamModules(t *testing.T) {
	for kind, want := range map[string]string{"terminal": "terminal", "logs": "logs", "apt": "apt", "docker-logs": "docker", "docker-pull": "docker",
		"docker-update": "docker", "docker-project-logs": "docker", "f2b-install": "security", "bogus": ""} {
		if got := streamModule(kind); got != want {
			t.Errorf("%s: got %q want %q", kind, got, want)
		}
	}
	for _, m := range RootModules {
		if !validModule(m) {
			t.Errorf("%s is not an operator module", m)
		}
	}
}
