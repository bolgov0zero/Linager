package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maxDuEntries = 500000
	duTimeout    = 20 * time.Second
)

// FileStat is the "Properties" view of one entry.
type FileStat struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"` // octal with the special bits, e.g. 0644 or 4755
	ModeStr    string `json:"mode_str"`
	Owner      string `json:"owner"`
	Group      string `json:"group"`
	UID        uint32 `json:"uid"`
	GID        uint32 `json:"gid"`
	MTime      int64  `json:"mtime"`
	ATime      int64  `json:"atime"`
	CTime      int64  `json:"ctime"` // last change of the inode (permissions, owner, content)
	BTime      int64  `json:"btime"` // creation time; 0 when the file system does not keep it
	Links      uint64 `json:"links"`
	Inode      uint64 `json:"inode"`
	LinkTarget string `json:"link_target,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Protected  bool   `json:"protected"` // a system directory: permissions and owner cannot be changed here
}

// birthTime asks stat(1) for the creation time (Go's standard library does not expose it).
func birthTime(ctx context.Context, path string) int64 {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "stat", "-c", "%W", "--", path).Output() //nolint:gosec // fixed command, the path follows "--"
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func filesStat(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fsErr(err)
	}
	e := entryOf(filepath.Dir(path), filepath.Base(path), info)
	st := FileStat{Name: e.Name, Path: path, Type: e.Type, Size: info.Size(), ModeStr: info.Mode().String(), Owner: e.Owner, Group: e.Group,
		MTime: info.ModTime().Unix(), LinkTarget: e.LinkTarget, Protected: protectedPaths[path]}
	if path == "/" {
		st.Name = "/"
	}
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		st.Mode = fmt.Sprintf("%04o", sys.Mode&0o7777)
		st.UID, st.GID, st.Links, st.Inode = sys.Uid, sys.Gid, uint64(sys.Nlink), sys.Ino //nolint:unconvert // Nlink is not uint64 everywhere
		st.ATime, st.CTime = statTimes(sys)
	} else {
		st.Mode = e.Mode
	}
	st.BTime = birthTime(ctx, path)
	if info.Mode().IsRegular() && info.Size() > 0 {
		if f, err := os.Open(path); err == nil { //nolint:gosec // a path chosen in the file manager
			buf := make([]byte, 512)
			n, _ := io.ReadFull(f, buf)
			_ = f.Close()
			st.Mime = http.DetectContentType(buf[:n])
		}
	}
	return st, nil
}

// filesDu adds up the size of a folder (regular files only, symlinks are not followed), with a time and entry limit.
func filesDu(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, duTimeout)
	defer cancel()
	var size, files, dirs int64
	truncated := false
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, werr error) error {
		if cctx.Err() != nil || files+dirs >= maxDuEntries {
			truncated = true
			return filepath.SkipAll
		}
		if werr != nil {
			return nil // unreadable entries are skipped
		}
		if d.IsDir() {
			dirs++
		} else if info, ierr := d.Info(); ierr == nil && info.Mode().IsRegular() {
			files++
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, fsErr(err)
	}
	return map[string]any{"size": size, "files": files, "dirs": dirs, "truncated": truncated}, nil
}

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
	if protectedPaths[path] {
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
	if protectedPaths[path] {
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
