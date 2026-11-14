package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/bolgov0zero/linager/internal/protocol"
)

const (
	maxEditSize   = 1 << 20 // text editor limit
	maxListing    = 5000
	maxUploadSize = 512 << 20
)

// FileEntry is one row of a directory listing.
type FileEntry struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModeStr    string `json:"mode_str"`
	Owner      string `json:"owner"`
	Group      string `json:"group"`
	MTime      int64  `json:"mtime"`
	LinkTarget string `json:"link_target,omitempty"`
}

var protectedPaths = map[string]bool{"/": true, "/bin": true, "/boot": true, "/dev": true, "/etc": true, "/home": true, "/lib": true, "/lib64": true,
	"/opt": true, "/proc": true, "/root": true, "/run": true, "/sbin": true, "/sys": true, "/usr": true, "/var": true}

// cleanPath validates and normalizes an absolute path.
func cleanPath(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", errors.New("invalid path")
	}
	return filepath.Clean(p), nil
}

var (
	idMu    sync.Mutex
	uidName = map[uint32]string{}
	gidName = map[uint32]string{}
)

func lookupUser(uid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := uidName[uid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	uidName[uid] = n
	return n
}

func lookupGroup(gid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := gidName[gid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	gidName[gid] = n
	return n
}

func entryOf(dir, name string, info fs.FileInfo) FileEntry {
	e := FileEntry{Name: name, Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), ModeStr: info.Mode().String(), MTime: info.ModTime().Unix()}
	switch {
	case info.IsDir():
		e.Type = "dir"
	case info.Mode()&fs.ModeSymlink != 0:
		e.Type = "link"
		if t, err := os.Readlink(filepath.Join(dir, name)); err == nil {
			e.LinkTarget = t
		}
	case info.Mode().IsRegular():
		e.Type = "file"
	default:
		e.Type = "other"
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		e.Owner, e.Group = lookupUser(st.Uid), lookupGroup(st.Gid)
	}
	return e
}

func filesList(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	dir, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fsErr(err)
	}
	out := make([]FileEntry, 0, len(ents))
	for _, d := range ents {
		if len(out) >= maxListing {
			break
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, entryOf(dir, d.Name(), info))
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Type == "dir") != (out[j].Type == "dir") {
			return out[i].Type == "dir"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return map[string]any{"path": dir, "entries": out}, nil
}

func fsErr(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New(protocol.ErrNotFound)
	case errors.Is(err, fs.ErrExist):
		return errors.New(protocol.ErrExists)
	case errors.Is(err, fs.ErrPermission):
		return errors.New("permission denied")
	}
	return err
}

func filesRead(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // the files module is a root file manager by design
	if err != nil {
		return nil, fsErr(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxEditSize {
		return nil, errors.New(protocol.ErrTooLarge)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxEditSize+1))
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return nil, errors.New(protocol.ErrBinary)
	}
	return map[string]any{"path": path, "content": string(b), "size": info.Size(), "mtime": info.ModTime().UnixMilli(), "mode": fmt.Sprintf("%04o", info.Mode().Perm())}, nil
}

// filesWrite replaces a file atomically, keeping mode and ownership.
func filesWrite(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Path    string
		Content string
		MTime   int64
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	if len(p.Content) > maxEditSize {
		return nil, errors.New(protocol.ErrTooLarge)
	}
	mode := os.FileMode(0o644)
	var uid, gid = -1, -1
	if st, err := os.Stat(path); err == nil {
		if !st.Mode().IsRegular() {
			return nil, errors.New("not a regular file")
		}
		if p.MTime != 0 && st.ModTime().UnixMilli() != p.MTime {
			return nil, errors.New(protocol.ErrConflict)
		}
		mode = st.Mode().Perm()
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(sys.Uid), int(sys.Gid)
		}
	} else if p.MTime != 0 {
		return nil, errors.New(protocol.ErrConflict)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".linager-edit-*")
	if err != nil {
		return nil, fsErr(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(p.Content); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	_ = os.Chmod(tmp.Name(), mode)
	if uid >= 0 {
		_ = os.Lchown(tmp.Name(), uid, gid)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, fsErr(err)
	}
	st, _ := os.Stat(path)
	return map[string]any{"ok": true, "mtime": st.ModTime().UnixMilli()}, nil
}

func filesMkdir(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0o755); err != nil { //nolint:gosec // directories of a file manager are world-readable by default
		return nil, fsErr(err)
	}
	return map[string]bool{"ok": true}, nil
}

func filesCreate(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) //nolint:gosec // see above
	if err != nil {
		return nil, fsErr(err)
	}
	_ = f.Close()
	return map[string]bool{"ok": true}, nil
}

func filesDelete(_ context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct{ Path string }
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
		return nil, errors.New("refusing to delete a system directory")
	}
	if err := os.RemoveAll(path); err != nil {
		return nil, fsErr(err)
	}
	return map[string]bool{"ok": true}, nil
}

// filesDownload streams a file to the server.
func filesDownload(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return err
	}
	f, err := os.Open(path) //nolint:gosec // see above
	if err != nil {
		return fsErr(err)
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	buf := make([]byte, 64<<10)
	for ctx.Err() == nil {
		n, err := f.Read(buf)
		if n > 0 {
			if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
				return oerr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	return nil
}

// filesUpload receives a file from the server; an empty chunk marks the end.
func filesUpload(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct{ Path string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return errors.New("target is a directory")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".linager-upload-*")
	if err != nil {
		return fsErr(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	var total int64
	for {
		select {
		case <-ctx.Done():
			_ = tmp.Close()
			return errors.New("upload cancelled")
		case b, ok := <-sio.In:
			if !ok {
				_ = tmp.Close()
				return errors.New("upload cancelled")
			}
			if len(b) == 0 { // end of file
				if err := tmp.Close(); err != nil {
					return err
				}
				mode := os.FileMode(0o644)
				if st, err := os.Stat(path); err == nil {
					mode = st.Mode().Perm()
				}
				_ = os.Chmod(tmp.Name(), mode)
				return fsErr(os.Rename(tmp.Name(), path))
			}
			total += int64(len(b))
			if total > maxUploadSize {
				_ = tmp.Close()
				return errors.New(protocol.ErrTooLarge)
			}
			if _, err := tmp.Write(b); err != nil {
				_ = tmp.Close()
				return err
			}
		}
	}
}
