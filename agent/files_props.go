package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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
	st := FileStat{Name: e.Name, Path: outPath(path), Type: e.Type, Size: info.Size(), ModeStr: info.Mode().String(), Owner: e.Owner, Group: e.Group,
		MTime: info.ModTime().Unix(), LinkTarget: e.LinkTarget, Protected: isProtected(path)}
	if path == "/" {
		st.Name = "/"
	}
	fillStat(info, &st, e.Mode)
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
