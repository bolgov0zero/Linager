package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func call(t *testing.T, h func(context.Context, json.RawMessage, bool) (any, error), dangerous bool, v any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(v)
	res, err := h(context.Background(), raw, dangerous)
	if err != nil {
		return nil, err
	}
	m, _ := res.(map[string]any)
	return m, nil
}

func TestFileCopyMoveRenameZip(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, d := range []string{a, b, filepath.Join(a, "dir")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(a, "f.txt"), "one")
	write(filepath.Join(a, "dir", "in.txt"), "inside")
	cp, mv := transfer(false), transfer(true)

	// copy: a conflict fails by default, keep-both picks a free name, overwrite needs the dangerous flag
	if _, err := call(t, cp, false, map[string]string{"src": filepath.Join(a, "f.txt"), "dir": b}); err != nil {
		t.Fatalf("plain copy: %v", err)
	}
	if _, err := call(t, cp, false, map[string]string{"src": filepath.Join(a, "f.txt"), "dir": b}); err == nil {
		t.Fatal("copying onto an existing file must fail by default")
	}
	res, err := call(t, cp, false, map[string]string{"src": filepath.Join(a, "f.txt"), "dir": b, "mode": "keep"})
	if err != nil || res["name"] != "f (2).txt" {
		t.Fatalf("keep both: %v %v", res, err)
	}
	write(filepath.Join(a, "f.txt"), "newer")
	if _, err := call(t, cp, false, map[string]string{"src": filepath.Join(a, "f.txt"), "dir": b, "mode": "overwrite"}); err == nil {
		t.Fatal("overwrite without confirmation must be refused")
	}
	if _, err := call(t, cp, true, map[string]string{"src": filepath.Join(a, "f.txt"), "dir": b, "mode": "overwrite"}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "f.txt")); string(got) != "newer" { //nolint:gosec // temp file of this test
		t.Fatalf("overwritten content = %q", got)
	}

	// a folder cannot go into itself; moving a folder keeps its content
	if _, err := call(t, mv, false, map[string]string{"src": filepath.Join(a, "dir"), "dir": filepath.Join(a, "dir")}); err == nil {
		t.Fatal("a folder inside itself must be refused")
	}
	if _, err := call(t, mv, false, map[string]string{"src": filepath.Join(a, "dir"), "dir": b}); err != nil {
		t.Fatalf("move folder: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "dir", "in.txt")); string(got) != "inside" { //nolint:gosec // temp file of this test
		t.Fatalf("moved content = %q", got)
	}
	if exists(filepath.Join(a, "dir")) {
		t.Fatal("the original must be gone after a move")
	}

	// rename never replaces an existing entry
	if _, err := call(t, filesRename, false, map[string]string{"path": filepath.Join(b, "f.txt"), "name": "f (2).txt"}); err == nil {
		t.Fatal("renaming onto an existing name must fail")
	}
	if _, err := call(t, filesRename, false, map[string]string{"path": filepath.Join(b, "f.txt"), "name": "g.txt"}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// zip of a file and a folder
	var buf bytes.Buffer
	raw, _ := json.Marshal(map[string]any{"dir": b, "names": []string{"g.txt", "dir"}})
	if err := filesZip(context.Background(), raw, &StreamIO{Out: func(p []byte) error { buf.Write(p); return nil }}); err != nil {
		t.Fatalf("zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["g.txt"] || !names["dir/"] || !names["dir/in.txt"] {
		t.Fatalf("zip entries = %v", names)
	}
}

func TestFileStatChmodDu(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "d")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "a.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": f})
	res, err := filesStat(context.Background(), raw, false)
	if err != nil {
		t.Fatal(err)
	}
	st := res.(FileStat)
	if st.Mode != "0600" || st.Size != 5 || st.Type != "file" || st.MTime == 0 || st.Links != 1 {
		t.Fatalf("stat: %+v", st)
	}
	// permissions: plain change works, setuid and recursive need the dangerous flag
	if _, err := call(t, filesChmod, false, map[string]any{"path": f, "mode": "0644"}); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if info, _ := os.Stat(f); info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v", info.Mode())
	}
	if _, err := call(t, filesChmod, false, map[string]any{"path": f, "mode": "4755"}); err == nil {
		t.Fatal("the setuid bit must need confirmation")
	}
	if _, err := call(t, filesChmod, false, map[string]any{"path": sub, "mode": "0750", "recursive": true}); err == nil {
		t.Fatal("a recursive change must need confirmation")
	}
	if _, err := call(t, filesChmod, true, map[string]any{"path": sub, "mode": "0750", "recursive": true}); err != nil {
		t.Fatalf("recursive chmod: %v", err)
	}
	if info, _ := os.Stat(f); info.Mode().Perm() != 0o750 {
		t.Fatalf("recursive mode = %v", info.Mode())
	}
	if _, err := call(t, filesChmod, true, map[string]any{"path": "/etc", "mode": "0777"}); err == nil {
		t.Fatal("a system directory must be refused")
	}
	if _, err := call(t, filesChown, false, map[string]any{"path": f, "owner": "0"}); err == nil {
		t.Fatal("chown must need confirmation")
	}
	du, err := filesDu(context.Background(), mustJSON(map[string]string{"path": root}), false)
	if err != nil || du.(map[string]any)["size"].(int64) != 5 || du.(map[string]any)["files"].(int64) != 1 {
		t.Fatalf("du: %v %v", du, err)
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
