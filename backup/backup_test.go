package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCreatesArchiveAndPrunes(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	cfg := filepath.Join(t.TempDir(), "server.yaml")
	_ = os.WriteFile(cfg, []byte("database: x\n"), 0o600)
	_ = os.WriteFile(filepath.Join(data, "ca.key"), []byte("KEY"), 0o600)
	var n int
	var last string
	for i := 0; i < 4; i++ {
		n++
		ts := time.Date(2026, 11, 14, 2, 0, i, 0, time.UTC)
		p, err := Run(context.Background(), Options{
			OutDir: dir, Files: []string{cfg}, DataDir: data, Keep: 3, Now: func() time.Time { return ts },
			DumpFunc: func(_ context.Context, _, dst string) error { return os.WriteFile(dst, []byte("DUMP"), 0o600) },
		})
		if err != nil {
			t.Fatal(err)
		}
		last = p
	}
	if got := List(dir, ""); len(got) != 3 {
		t.Fatalf("keep=3 must prune to 3 files, got %d", len(got))
	}
	if st, _ := os.Stat(last); st.Mode().Perm() != 0o600 {
		t.Fatalf("backups hold secrets and must be 0600, got %v", st.Mode().Perm())
	}
	f, _ := os.Open(last) //nolint:gosec // test fixture
	gz, _ := gzip.NewReader(f)
	tr := tar.NewReader(gz)
	names := map[string]string{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(tr)
		names[h.Name] = string(b)
	}
	if names["db.dump"] != "DUMP" || names["config/server.yaml"] != "database: x\n" || names["data/ca.key"] != "KEY" {
		t.Fatalf("archive content: %v", names)
	}
	if name, _, err := Last(dir, ""); err != nil || name != filepath.Base(last) {
		t.Fatalf("Last: %s %v", name, err)
	}
}
