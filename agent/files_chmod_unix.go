//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// goMode converts an octal permission value (with the setuid, setgid and sticky bits) to a Go file mode.
func goMode(v uint32) fs.FileMode {
	m := fs.FileMode(v & 0o777)
	if v&0o4000 != 0 {
		m |= fs.ModeSetuid
	}
	if v&0o2000 != 0 {
		m |= fs.ModeSetgid
	}
	if v&0o1000 != 0 {
		m |= fs.ModeSticky
	}
	return m
}

// walkApply runs fn on the path and, when recursive, on everything below it (symlinks are passed too).
func walkApply(path string, recursive bool, fn func(string, fs.FileInfo) error) error {
	if !recursive {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		return fn(path, info)
	}
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(p, info)
	})
}

// filesChmod sets the permission bits. Recursive changes and the setuid, setgid and sticky bits need confirmation.
func filesChmod(_ context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct {
		Path      string
		Mode      string
		Recursive bool
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(p.Mode), 8, 32)
	if err != nil || v > 0o7777 {
		return nil, errors.New("invalid mode")
	}
	if isProtected(path) {
		return nil, errors.New("refusing to change a system directory")
	}
	if (p.Recursive || v&0o7000 != 0) && !dangerous {
		return nil, errDangerous
	}
	mode := goMode(uint32(v))
	if err := walkApply(path, p.Recursive, func(pth string, info fs.FileInfo) error {
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil // a link has no permissions of its own
		}
		return os.Chmod(pth, mode)
	}); err != nil {
		return nil, fsErr(err)
	}
	return map[string]bool{"ok": true}, nil
}

func lookupIDs(owner, group string) (uid, gid int, err error) {
	uid, gid = -1, -1
	if o := strings.TrimSpace(owner); o != "" {
		if n, perr := strconv.Atoi(o); perr == nil && n >= 0 {
			uid = n
		} else if u, lerr := user.Lookup(o); lerr == nil {
			uid, _ = strconv.Atoi(u.Uid)
		} else {
			return 0, 0, fmt.Errorf("unknown user %q", o)
		}
	}
	if g := strings.TrimSpace(group); g != "" {
		if n, perr := strconv.Atoi(g); perr == nil && n >= 0 {
			gid = n
		} else if gr, lerr := user.LookupGroup(g); lerr == nil {
			gid, _ = strconv.Atoi(gr.Gid)
		} else {
			return 0, 0, fmt.Errorf("unknown group %q", g)
		}
	}
	return uid, gid, nil
}

// filesChown changes the owner and group (empty = unchanged). It always needs confirmation.
func filesChown(_ context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct {
		Path      string
		Owner     string
		Group     string
		Recursive bool
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	if !dangerous {
		return nil, errDangerous
	}
	if isProtected(path) {
		return nil, errors.New("refusing to change a system directory")
	}
	uid, gid, err := lookupIDs(p.Owner, p.Group)
	if err != nil {
		return nil, err
	}
	if uid < 0 && gid < 0 {
		return nil, errors.New("nothing to change")
	}
	if err := walkApply(path, p.Recursive, func(pth string, _ fs.FileInfo) error {
		return os.Lchown(pth, uid, gid)
	}); err != nil {
		return nil, fsErr(err)
	}
	return map[string]bool{"ok": true}, nil
}
