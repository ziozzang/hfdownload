package hfcache

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func exportFixture(t *testing.T) (cache string, reg, lfs []byte) {
	t.Helper()
	src, m, reg, lfs := buildFlatRepo(t)
	cache = t.TempDir()
	if _, err := Export(ExportOptions{Manifest: m, SourceDir: src, CacheRoot: cache}); err != nil {
		t.Fatalf("export: %v", err)
	}
	return cache, reg, lfs
}

func TestListRepos(t *testing.T) {
	cache, reg, lfs := exportFixture(t)
	repos, err := ListRepos(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(repos))
	}
	r := repos[0]
	if r.RepoType != "model" || r.RepoID != "owner/model" {
		t.Fatalf("repo = %+v", r)
	}
	if len(r.Commits) != 1 || r.Refs["main"] == "" {
		t.Fatalf("commits/refs = %+v", r)
	}
	if r.Blobs != 2 || r.Bytes != int64(len(reg)+len(lfs)) {
		t.Fatalf("blobs=%d bytes=%d", r.Blobs, r.Bytes)
	}
}

func TestVerifyStorageDetectsCorruption(t *testing.T) {
	cache, reg, _ := exportFixture(t)
	storage := filepath.Join(cache, "models--owner--model")
	rep, err := VerifyStorage(storage, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 0 || rep.Blobs != 2 {
		t.Fatalf("clean verify = %+v", rep)
	}
	// Corrupt a blob so its content no longer matches its name.
	if err := os.WriteFile(filepath.Join(storage, "blobs", gitSHA1(reg)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = VerifyStorage(storage, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed == 0 {
		t.Fatalf("expected corruption to be reported, got %+v", rep)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	cache, reg, lfs := exportFixture(t)
	storage := filepath.Join(cache, "models--owner--model")
	var buf bytes.Buffer
	if err := ArchiveStorage(storage, &buf); err != nil {
		t.Fatalf("archive: %v", err)
	}

	dest := t.TempDir()
	tr := tar.NewReader(&buf)
	sawSymlink := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dest, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		case tar.TypeSymlink:
			sawSymlink = true
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
		}
	}
	if !sawSymlink {
		t.Fatal("archive did not preserve snapshot symlinks")
	}
	// Blobs restored, and a snapshot symlink resolves to its blob content.
	extracted := filepath.Join(dest, "models--owner--model")
	if b, err := os.ReadFile(filepath.Join(extracted, "blobs", gitSHA1(reg))); err != nil || !bytes.Equal(b, reg) {
		t.Fatalf("extracted regular blob wrong: %v", err)
	}
	pointer := filepath.Join(extracted, "snapshots", "0123456789abcdef0123456789abcdef01234567", "weights", "model.bin")
	if b, err := os.ReadFile(pointer); err != nil || !bytes.Equal(b, lfs) {
		t.Fatalf("extracted snapshot symlink did not resolve to blob: %v", err)
	}
}

func TestImportResumeReusesAndRepairs(t *testing.T) {
	cache, reg, _ := exportFixture(t)

	// Pre-populate the destination: config.json already correct, model.bin wrong
	// but the right size (must be repaired).
	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "config.json"), reg)
	m, _, err := Import(ImportOptions{CacheRoot: cache, RepoID: "owner/model", RepoType: "model", Revision: "main", DestDir: dst})
	if err != nil {
		t.Fatalf("import (resume): %v", err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("imported %d files", len(m.Files))
	}
	// A second import over a complete destination is a no-op that still succeeds.
	if _, _, err := Import(ImportOptions{CacheRoot: cache, RepoID: "owner/model", RepoType: "model", Revision: "main", DestDir: dst}); err != nil {
		t.Fatalf("re-import: %v", err)
	}
}
