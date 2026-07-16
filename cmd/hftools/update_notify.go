package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ziozzang/hftools/internal/selfupdate"
)

// updateCheckInterval throttles how often the background version check hits the
// network. Between checks the cached result is reused.
const updateCheckInterval = 24 * time.Hour

// notifyExemptCommands are commands for which an update notice would be noise or
// redundant.
var notifyExemptCommands = map[string]bool{
	"": true, "update": true, "self-update": true, "version": true,
	"--version": true, "-version": true, "-v": true, "-V": true,
	"help": true, "-h": true, "--help": true, "completion": true,
}

type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest_version"`
}

// updateCheckEligible reports whether the update machinery should run at all: it
// stays silent in scripts and pipes (non-terminal stderr), when disabled via
// HFTOOLS_NO_UPDATE_CHECK, and for exempt commands.
func updateCheckEligible(args []string) bool {
	if os.Getenv("HFTOOLS_NO_UPDATE_CHECK") != "" {
		return false
	}
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	if notifyExemptCommands[cmd] {
		return false
	}
	return stderrIsTerminal()
}

// startUpdateRefresh kicks off a background version check when the cached result
// is stale, then returns immediately. The foreground never waits on it, so an
// offline or air-gapped machine is never slowed down: the goroutine simply times
// out and soft-fails, writing nothing. A successful check updates the cache that
// the notice reads (this run if it finishes in time, otherwise the next).
func startUpdateRefresh(args []string) {
	if !updateCheckEligible(args) {
		return
	}
	path := updateCachePath()
	c := readUpdateCache(path)
	if c.Latest != "" && time.Since(c.CheckedAt) < updateCheckInterval {
		return // cached result is still fresh; no network needed
	}
	go func() {
		// Its own context so a fast foreground command finishing does not cancel
		// the check prematurely; the process exiting ends it regardless.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rel, err := selfupdate.LatestRelease(ctx, &http.Client{}, "", updateRepo, os.Getenv("GITHUB_TOKEN"))
		if err != nil {
			return // soft-fail: offline/air-gapped or GitHub down -> do nothing
		}
		writeUpdateCache(path, updateCache{CheckedAt: time.Now(), Latest: rel.Version()})
	}()
}

// maybeNotifyUpdate prints a one-line notice built from the cached latest
// version. It performs no network I/O, so it is instant and safe offline.
func maybeNotifyUpdate(args []string) {
	if !updateCheckEligible(args) {
		return
	}
	c := readUpdateCache(updateCachePath())
	if c.Latest == "" {
		return
	}
	if notice := updateNoticeText(c.Latest, version); notice != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", notice)
	}
}

// updateNoticeText returns the upgrade notice when latest is newer than
// current, or "" otherwise.
func updateNoticeText(latest, current string) string {
	if selfupdate.CompareVersions(latest, current) > 0 {
		return fmt.Sprintf("hftools %s is available (you have %s). Run 'hftools update' to upgrade.", latest, current)
	}
	return ""
}

func updateCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "hftools", "update-check.json")
}

func readUpdateCache(path string) updateCache {
	var c updateCache
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func writeUpdateCache(path string, c updateCache) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	// Write atomically so a concurrent hftools process never reads a half file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-check-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, path)
}

// stderrIsTerminal reports whether stderr is a character device (a terminal),
// which we use to suppress the notice when output is redirected to a file/pipe.
func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
