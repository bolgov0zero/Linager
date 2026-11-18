//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Removing a key from authorized_keys. The browser names the user, the file and the fingerprint, and the
// agent accepts the request only when the file is one of the files sshd reads for that user (the same
// list the Security tab shows), so a path from the browser never selects anything else. A copy of the
// file is saved next to it before it is changed, and the owner and the mode of the file stay the same.

var fingerprintRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// removeKeyByFingerprint drops the key lines with the fingerprint, keeping comments and other lines as they are.
func removeKeyByFingerprint(content, fingerprint string) (string, int) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	removed := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line != "" && !strings.HasPrefix(line, "#") {
			if _, typ, blob, _, ok := splitKeyLine(line); ok {
				if fp, _, ok := keyInfo(typ, blob); ok && fp == fingerprint {
					removed++
					continue
				}
			}
		}
		out = append(out, raw)
	}
	return strings.Join(out, "\n"), removed
}

// allowedKeyFiles are the authorized_keys files of the user that the agent knows about.
func (m *secModule) allowedKeyFiles(ctx context.Context, user string) ([]string, error) {
	conf := m.sshConfig(ctx).Raw
	patterns := strings.Fields(conf["authorizedkeysfile"])
	if len(patterns) == 0 {
		patterns = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	}
	b, err := m.readFile(m.passwdPath)
	if err != nil {
		return nil, err
	}
	for _, u := range parsePasswd(string(b)) {
		if u.Name != user {
			continue
		}
		var files []string
		for _, pat := range patterns {
			files = append(files, expandKeyPath(pat, u.Home, u.Name))
		}
		return files, nil
	}
	return nil, errors.New("no such user")
}

// keyRemove is the command "security.keyRemove": {user, file, fingerprint}.
func (m *secModule) keyRemove(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct{ User, File, Fingerprint string }
	if err := json.Unmarshal(raw, &p); err != nil || !userNameRe.MatchString(p.User) || !fingerprintRe.MatchString(p.Fingerprint) {
		return nil, errors.New("invalid request")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	files, err := m.allowedKeyFiles(ctx, p.User)
	if err != nil {
		return nil, err
	}
	if !containsStr(files, p.File) {
		return nil, errors.New("this is not an authorized_keys file of the user")
	}
	b, err := m.readFile(p.File)
	if err != nil {
		return nil, err
	}
	next, n := removeKeyByFingerprint(string(b), p.Fingerprint)
	if n == 0 {
		return nil, errors.New("the key is not in the file any more")
	}
	backup := p.File + ".bak-" + m.now().Format("20060102-150405")
	if err := m.writeKeyFile(backup, b, p.File); err != nil {
		return nil, fmt.Errorf("the copy of the file was not saved, nothing was changed: %w", err)
	}
	if err := m.writeKeyFile(p.File, []byte(next), p.File); err != nil {
		return nil, err
	}
	m.forget()
	return map[string]any{"ok": true, "removed": n, "backup": backup}, nil
}

// defaultWriteKeyFile writes a file with the owner and the mode of the reference file, through a temporary file.
func defaultWriteKeyFile(path string, data []byte, ref string) error {
	st, err := os.Stat(ref)
	if err != nil {
		return err
	}
	tmp := path + ".linager-new"
	if err := os.WriteFile(tmp, data, st.Mode().Perm()); err != nil { //nolint:gosec // the mode mirrors the existing file
		return err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(tmp, int(sys.Uid), int(sys.Gid)); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Chmod(tmp, st.Mode().Perm()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
