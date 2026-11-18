package server

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/bolgov0zero/linager/internal/pki"
	"github.com/bolgov0zero/linager/internal/version"
	"github.com/bolgov0zero/linager/internal/winjoin"
	"github.com/bolgov0zero/linager/update"
)

// Install of the Windows agent by a link. The new computer runs one PowerShell command; it checks the certificate of this
// server by its fingerprint, downloads the installer package, compares it with the signed release, hands the
// one-time code of the link to the service through a file only the administrators can read, and installs.

// joinMSI serves the installer package of this server's release to a host that came with a valid link.
func (a *AgentPort) joinMSI(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.joinToken(w, r); !ok {
		return
	}
	if a.Updates == nil {
		http.Error(w, "releases are not available", http.StatusServiceUnavailable)
		return
	}
	c, err := a.Updates.CurrentRelease(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	ma, ok := c.Manifest.WindowsMSI()
	if !ok {
		http.Error(w, "this release has no Windows installer", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(c.Dir, ma.Name)) //nolint:gosec // a verified cache entry and a name from its manifest
}

// joinWindowsScript serves the PowerShell script that the install command runs. It is plain text a person can read first.
func (a *AgentPort) joinWindowsScript(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.joinToken(w, r); !ok {
		return
	}
	msiSum := ""
	if a.Updates != nil {
		if c, err := update.LoadCached(a.Updates.Key, a.Updates.Cache, version.Version); err == nil {
			if ma, ok := c.Manifest.WindowsMSI(); ok {
				msiSum = ma.SHA256
			}
		}
	}
	if msiSum == "" {
		http.Error(w, "this release has no Windows installer", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprint(w, winjoin.ScriptText(a.serverAddr(r), pki.Fingerprint(a.Keys.TLSDER), pki.Fingerprint(a.Keys.TLSAltDER), r.PathValue("token"), msiSum)) //nolint:gosec // plain text for a shell: the token is checked above, the address is made safe by serverAddr, the fingerprint is base64
}
