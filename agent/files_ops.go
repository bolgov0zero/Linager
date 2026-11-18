package agent

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// Conflict modes of copy and move when the target already exists.
const (
	modeFail      = "fail"
	modeOverwrite = "overwrite"
	modeKeep      = "keep" // keep both: the new one gets a free name like "report (2).txt"
)

const maxZipNames = 1000

// validBaseName reports whether s is a single, usable file name.
func validBaseName(s string) bool {
	return s != "" && s != "." && s != ".." && len(s) <= 255 && !strings.ContainsAny(s, "/\x00")
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// uniqueName returns "name (2).ext", "name (3).ext", ... that does not exist in dir.
func uniqueName(dir, name string, isDir bool) string {
	ext := ""
	stem := name
	if !isDir {
		ext = filepath.Ext(name)
		stem = strings.TrimSuffix(name, ext)
	}
	for i := 2; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if !exists(filepath.Join(dir, cand)) {
			return cand
		}
	}
	return fmt.Sprintf("%s (%d)%s", stem, time.Now().UnixNano(), ext)
}

// filesRename renames an entry inside its directory; an existing target is never replaced.
func filesRename(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ Path, Name string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	if !validBaseName(p.Name) {
		return nil, errors.New("invalid name")
	}
	if isProtected(path) {
		return nil, errors.New("refusing to rename a system directory")
	}
	dst := filepath.Join(filepath.Dir(path), p.Name)
	if dst == path {
		return map[string]any{"ok": true, "name": p.Name}, nil
	}
	if !exists(path) {
		return nil, errors.New(protocol.ErrNotFound)
	}
	if exists(dst) {
		return nil, errors.New(protocol.ErrExists)
	}
	if err := os.Rename(path, dst); err != nil {
		return nil, fsErr(err)
	}
	return map[string]any{"ok": true, "name": p.Name}, nil
}

// transfer returns the handler of files.copy (move=false) or files.move (move=true): src goes into dir under its own
// name. mode decides what happens when the target exists; overwrite needs the dangerous flag.
func transfer(move bool) func(context.Context, json.RawMessage, bool) (any, error) {
	return func(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
		var p struct{ Src, Dir, Mode string }
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errors.New("invalid request")
		}
		src, err := cleanPath(p.Src)
		if err != nil {
			return nil, err
		}
		dir, err := cleanPath(p.Dir)
		if err != nil {
			return nil, err
		}
		mode := p.Mode
		switch mode {
		case "", modeFail:
			mode = modeFail
		case modeOverwrite:
			if !dangerous {
				return nil, errDangerous
			}
		case modeKeep:
		default:
			return nil, errors.New("invalid mode")
		}
		if isProtected(src) {
			return nil, errors.New("refusing to move or copy a system directory")
		}
		st, err := os.Lstat(src)
		if err != nil {
			return nil, fsErr(err)
		}
		if d, err := os.Stat(dir); err != nil || !d.IsDir() {
			return nil, errors.New("target is not a directory")
		}
		if st.IsDir() && (dir == src || strings.HasPrefix(dir+"/", src+"/")) {
			return nil, errors.New("cannot place a folder inside itself")
		}
		name := filepath.Base(src)
		target := filepath.Join(dir, name)
		if target == src {
			switch {
			case move:
				return map[string]any{"ok": true, "name": name}, nil
			case mode == modeKeep:
				target = filepath.Join(dir, uniqueName(dir, name, st.IsDir()))
			case mode == modeOverwrite:
				return nil, errors.New("source and target are the same")
			default:
				return nil, errors.New(protocol.ErrExists)
			}
		} else if exists(target) {
			switch mode {
			case modeKeep:
				target = filepath.Join(dir, uniqueName(dir, name, st.IsDir()))
			case modeOverwrite:
				if isProtected(target) || strings.HasPrefix(src+string(filepath.Separator), target+string(filepath.Separator)) {
					return nil, errors.New("refusing to replace this target")
				}
				if err := os.RemoveAll(target); err != nil {
					return nil, fsErr(err)
				}
			default:
				return nil, errors.New(protocol.ErrExists)
			}
		}
		if move {
			if err := os.Rename(src, target); err != nil {
				if !isCrossDevice(err) {
					return nil, fsErr(err)
				}
				// another file system: copy, then remove the original
				if err := copyTree(ctx, src, target); err != nil {
					_ = os.RemoveAll(target)
					return nil, fsErr(err)
				}
				if err := os.RemoveAll(src); err != nil {
					return nil, fsErr(err)
				}
			}
		} else if err := copyTree(ctx, src, target); err != nil {
			_ = os.RemoveAll(target)
			return nil, fsErr(err)
		}
		return map[string]any{"ok": true, "name": filepath.Base(target)}, nil
	}
}

// copyTree copies a file, directory tree or symlink keeping mode, owner (best effort) and modification time.
func copyTree(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		link, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(link, dst); err != nil {
			return err
		}
		keepOwner(dst, st, true)
	case st.IsDir():
		if err := os.Mkdir(dst, 0o700); err != nil {
			return err
		}
		ents, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := copyTree(ctx, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		_ = os.Chmod(dst, st.Mode().Perm())
		keepOwner(dst, st, false)
		_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	case st.Mode().IsRegular():
		if err := copyFile(src, dst, st); err != nil {
			return err
		}
	default:
		// sockets, devices and pipes are not copied
	}
	return nil
}

func copyFile(src, dst string, st fs.FileInfo) error {
	in, err := os.Open(src) //nolint:gosec // a path chosen in the file manager
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // same
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Chmod(dst, st.Mode().Perm())
	keepOwner(dst, st, false)
	_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	return nil
}

// chunkWriter sends what is written as stream chunks.
type chunkWriter struct{ out func([]byte) error }

func (c chunkWriter) Write(b []byte) (int, error) {
	const chunk = 64 << 10
	n := 0
	for len(b) > 0 {
		part := b
		if len(part) > chunk {
			part = part[:chunk]
		}
		if err := c.out(append([]byte(nil), part...)); err != nil {
			return n, err
		}
		n += len(part)
		b = b[len(part):]
	}
	return n, nil
}

// filesZip streams a zip archive of the named entries of a directory (folders recursively; only regular files and
// folders are stored).
func filesZip(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Dir   string
		Names []string
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return errors.New("invalid request")
	}
	dir, err := cleanPath(p.Dir)
	if err != nil {
		return err
	}
	if len(p.Names) == 0 || len(p.Names) > maxZipNames {
		return errors.New("invalid request")
	}
	for _, n := range p.Names {
		if !validBaseName(n) {
			return errors.New("invalid name")
		}
	}
	zw := zip.NewWriter(chunkWriter{out: sio.Out})
	for _, name := range p.Names {
		root := filepath.Join(dir, name)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				h, err := zip.FileInfoHeader(info)
				if err != nil {
					return err
				}
				h.Name = filepath.ToSlash(rel) + "/"
				h.Method = zip.Store
				_, err = zw.CreateHeader(h)
				return err
			case info.Mode().IsRegular():
				h, err := zip.FileInfoHeader(info)
				if err != nil {
					return err
				}
				h.Name = filepath.ToSlash(rel)
				h.Method = zip.Deflate
				w, err := zw.CreateHeader(h)
				if err != nil {
					return err
				}
				f, err := os.Open(path) //nolint:gosec // a path chosen in the file manager
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				_, err = io.Copy(w, f)
				return err
			}
			return nil
		})
		if err != nil {
			return fsErr(err)
		}
	}
	return zw.Close()
}
