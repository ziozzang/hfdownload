package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ziozzang/hfdownload/internal/hfcache"
	"github.com/ziozzang/hfdownload/internal/hub"
	"github.com/ziozzang/hfdownload/internal/state"
)

// cacheExportCommand converts an hfdown download directory into the
// huggingface_hub cache layout so it can be used offline by HF libraries.
func cacheExportCommand(args []string) error {
	fs := flag.NewFlagSet("cache-export", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", ".", "hfdown download directory to export")
	cache := fs.String("cache", "", "HF cache root (default: $HF_HUB_CACHE, $HF_HOME/hub, or ~/.cache/huggingface/hub)")
	copyBlobs := fs.Bool("copy", false, "copy blobs instead of hardlinking them from the source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	stateDir, err := stateDirectory(root)
	if err != nil {
		return err
	}
	m, err := state.LoadManifest(filepath.Join(stateDir, "manifest.json"))
	if err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("no hfdown manifest in %s", root)
	}
	res, err := hfcache.Export(hfcache.ExportOptions{
		Manifest: m, SourceDir: root, CacheRoot: *cache, Copy: *copyBlobs, BufferSize: 1 << 20,
	})
	if err != nil {
		return err
	}
	for _, s := range res.SkippedMsg {
		fmt.Fprintf(os.Stderr, "skip: %s\n", s)
	}
	fmt.Printf("cache-export: repo=%s commit=%s files=%d new-blobs=%d size=%s\n", m.RepoID, m.CommitSHA, res.Files, res.NewBlobs, humanBytes(res.Bytes))
	fmt.Printf("cache root: %s\nrepository: %s\n", res.CacheRoot, res.Storage)
	return nil
}

// cacheImportCommand converts a huggingface_hub cache snapshot into hfdown's
// flat layout, hashing and verifying every file and writing a fresh manifest.
func cacheImportCommand(args []string) error {
	fs := flag.NewFlagSet("cache-import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "destination directory (default: <owner>_<repo>)")
	cache := fs.String("cache", "", "HF cache root (default: $HF_HUB_CACHE, $HF_HOME/hub, or ~/.cache/huggingface/hub)")
	repo := fs.String("repo", "", "repository ID or URL (owner/name)")
	repoType := fs.String("type", "model", "repository type: model or dataset")
	revision := fs.String("revision", "main", "branch, tag, or commit to import")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		return fmt.Errorf("cache-import requires --repo OWNER/NAME")
	}
	repoID, err := hub.NormalizeRepoID(*repo)
	if err != nil {
		return err
	}
	if err := hub.RepoType(*repoType).Validate(); err != nil {
		return err
	}
	out := *output
	if out == "" {
		out = hub.LocalDirectoryName(repoID)
	}
	root, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	m, res, err := hfcache.Import(hfcache.ImportOptions{
		CacheRoot: *cache, RepoID: repoID, RepoType: *repoType, Revision: *revision, DestDir: root, BufferSize: 1 << 20,
	})
	if err != nil {
		return err
	}
	stateDir, err := stateDirectory(root)
	if err != nil {
		return err
	}
	if err := saveDownloadCheckpoint(filepath.Join(stateDir, "manifest.json"), root, m); err != nil {
		return err
	}
	fmt.Printf("cache-import: repo=%s commit=%s files=%d size=%s\n", repoID, res.Commit, res.Files, humanBytes(res.Bytes))
	fmt.Printf("source: %s\nsaved to %s\n", res.Storage, root)
	return nil
}
