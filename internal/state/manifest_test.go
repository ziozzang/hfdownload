package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Manifest ---

func TestNewManifest(t *testing.T) {
	m := NewManifest("user/model", "main", "abc123")
	if m.Version != ManifestVersion {
		t.Fatalf("expected version %d, got %d", ManifestVersion, m.Version)
	}
	if m.RepoID != "user/model" {
		t.Fatalf("expected RepoID 'user/model', got '%s'", m.RepoID)
	}
	if m.Revision != "main" {
		t.Fatalf("expected Revision 'main', got '%s'", m.Revision)
	}
	if m.CommitSHA != "abc123" {
		t.Fatalf("expected CommitSHA 'abc123', got '%s'", m.CommitSHA)
	}
	if m.Files == nil {
		t.Fatal("expected Files map to be initialized")
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := NewManifest("user/model", "main", "abc123")
	m.Files["file.txt"] = &FileRecord{
		Path:        "file.txt",
		Size:        100,
		LocalSHA256: "deadbeef",
	}

	if err := SaveJSONAtomic(path, m); err != nil {
		t.Fatalf("SaveJSONAtomic: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil manifest")
	}
	if loaded.RepoID != "user/model" {
		t.Fatalf("expected RepoID 'user/model', got '%s'", loaded.RepoID)
	}
	if loaded.Files["file.txt"] == nil {
		t.Fatal("expected file.txt to exist")
	}
	if loaded.Files["file.txt"].LocalSHA256 != "deadbeef" {
		t.Fatalf("expected LocalSHA256 'deadbeef', got '%s'", loaded.Files["file.txt"].LocalSHA256)
	}
}

func TestLoadManifestNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if m != nil {
		t.Fatal("expected nil manifest for nonexistent file")
	}
}

func TestLoadManifestBadVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	data := `{"version": 999, "repo_id": "user/model"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestLoadManifestBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestLoadManifestNilFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	data := `{"version": 1, "repo_id": "user/model", "revision": "main", "commit_sha": "abc"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Files == nil {
		t.Fatal("expected Files map to be initialized after load")
	}
}

// --- WriteFileAtomic ---

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	if err := WriteFileAtomic(path, data, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %q, got %q", data, got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestWriteFileAtomicNestedDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "test.txt")

	if err := WriteFileAtomic(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := WriteFileAtomic(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("expected 'new', got '%s'", got)
	}
}

func TestWriteFileAtomicConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte(strings.Repeat("a", n+1))
			_ = WriteFileAtomic(path, data, 0o600)
		}(i)
	}
	wg.Wait()

	// File should exist and be readable (no partial/corrupt write)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file should be readable after concurrent writes: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("file should not be empty")
	}
}

// --- SaveJSONAtomic ---

func TestSaveJSONAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	type person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	if err := SaveJSONAtomic(path, person{Name: "Alice", Age: 30}); err != nil {
		t.Fatalf("SaveJSONAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Should end with newline
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Fatal("expected trailing newline in JSON output")
	}

	var p person
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Name != "Alice" || p.Age != 30 {
		t.Fatalf("expected Alice/30, got %s/%d", p.Name, p.Age)
	}
}

func TestSaveJSONAtomicWithManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := NewManifest("user/dataset", "main", "sha")
	m.UpdatedAt = time.Now()
	m.Files["data.csv"] = &FileRecord{
		Path:        "data.csv",
		Size:        1024,
		LocalSHA256: "hash123",
	}

	if err := SaveJSONAtomic(path, m); err != nil {
		t.Fatalf("SaveJSONAtomic: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Files["data.csv"].Size != 1024 {
		t.Fatalf("expected size 1024, got %d", loaded.Files["data.csv"].Size)
	}
}

// --- AppendJSONLine / AppendHistory ---

func TestAppendJSONLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	h1 := VerifyHistory{RepoID: "user/model", CommitSHA: "abc", Passed: 10, Failed: 0}
	h2 := VerifyHistory{RepoID: "user/model", CommitSHA: "def", Passed: 5, Failed: 1}

	if err := AppendJSONLine(path, h1); err != nil {
		t.Fatalf("AppendJSONLine 1: %v", err)
	}
	if err := AppendJSONLine(path, h2); err != nil {
		t.Fatalf("AppendJSONLine 2: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var loaded1, loaded2 VerifyHistory
	if err := json.Unmarshal([]byte(lines[0]), &loaded1); err != nil {
		t.Fatalf("Unmarshal line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &loaded2); err != nil {
		t.Fatalf("Unmarshal line 2: %v", err)
	}

	if loaded1.CommitSHA != "abc" || loaded2.CommitSHA != "def" {
		t.Fatalf("expected abc/def, got %s/%s", loaded1.CommitSHA, loaded2.CommitSHA)
	}
}

func TestAppendHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")

	h := VerifyHistory{
		StartedAt: time.Now(),
		RepoID:    "user/model",
		Passed:    5,
		Failed:    0,
		Checked:   5,
	}

	if err := AppendHistory(path, h); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("file should not be empty")
	}

	// Should be valid JSON on a single line
	var loaded VerifyHistory
	if err := json.Unmarshal(got, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if loaded.RepoID != "user/model" {
		t.Fatalf("expected RepoID 'user/model', got '%s'", loaded.RepoID)
	}
}

// --- SortedFiles ---

func TestSortedFiles(t *testing.T) {
	m := NewManifest("user/model", "main", "abc")
	m.Files["c.txt"] = &FileRecord{Path: "c.txt"}
	m.Files["a.txt"] = &FileRecord{Path: "a.txt"}
	m.Files["b.txt"] = &FileRecord{Path: "b.txt"}

	sorted := SortedFiles(m)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 files, got %d", len(sorted))
	}
	if sorted[0].Path != "a.txt" || sorted[1].Path != "b.txt" || sorted[2].Path != "c.txt" {
		t.Fatalf("expected a/b/c order, got %s/%s/%s",
			sorted[0].Path, sorted[1].Path, sorted[2].Path)
	}
}

func TestSortedFilesEmpty(t *testing.T) {
	m := NewManifest("user/model", "main", "abc")
	sorted := SortedFiles(m)
	if len(sorted) != 0 {
		t.Fatalf("expected 0 files, got %d", len(sorted))
	}
}

// --- Checksum files ---

func TestWriteChecksumFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.sha256")

	m := NewManifest("user/model", "main", "abc")
	m.Files["model.bin"] = &FileRecord{
		Path:        "model.bin",
		LocalSHA256: "abcdef1234567890",
	}
	m.Files["readme.md"] = &FileRecord{
		Path:        "readme.md",
		LocalSHA256: "0987654321fedcba",
	}
	m.Files["unverified.txt"] = &FileRecord{
		Path:              "unverified.txt",
		LocalSHA256:       "something",
		VerificationError: "failed",
	}

	if err := WriteChecksumFile(path, m); err != nil {
		t.Fatalf("WriteChecksumFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	output := string(got)

	if !strings.Contains(output, "abcdef1234567890  model.bin") {
		t.Fatal("expected model.bin checksum in output")
	}
	if !strings.Contains(output, "0987654321fedcba  readme.md") {
		t.Fatal("expected readme.md checksum in output")
	}
	if strings.Contains(output, "unverified.txt") {
		t.Fatal("unverified file should be skipped")
	}
}

func TestWriteSHA1ChecksumFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.sha1")

	m := NewManifest("user/model", "main", "abc")
	m.Files["model.bin"] = &FileRecord{
		Path:      "model.bin",
		LocalSHA1: "sha1hash",
	}

	if err := WriteSHA1ChecksumFile(path, m); err != nil {
		t.Fatalf("WriteSHA1ChecksumFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "SHA-1") {
		t.Fatal("expected SHA-1 algorithm header")
	}
	if !strings.Contains(string(got), "sha1hash  model.bin") {
		t.Fatal("expected sha1 hash in output")
	}
}

func TestWriteChecksumFileDefaultRepoType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.sha256")

	m := NewManifest("user/model", "main", "abc")
	// RepoType left empty — should default to "model"
	m.Files["file.bin"] = &FileRecord{
		Path:        "file.bin",
		LocalSHA256: "hash",
	}

	if err := WriteChecksumFile(path, m); err != nil {
		t.Fatalf("WriteChecksumFile: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# type: model") {
		t.Fatalf("expected default type 'model', got: %s", string(got))
	}
}
