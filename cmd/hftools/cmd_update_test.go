package main

import (
	"testing"

	"github.com/ziozzang/hftools/internal/hub"
	"github.com/ziozzang/hftools/internal/state"
)

// remoteMatchesRecord must answer "did upstream change this file?" independently
// of local disk state, so a merely touched file is not reported as changed.
func TestRemoteMatchesRecord(t *testing.T) {
	remote := hub.RepoFile{Path: "model.bin", Size: 100, BlobID: "blob1"}
	rec := &state.FileRecord{Path: "model.bin", Size: 100, RemoteBlobSHA1: "blob1"}

	if !remoteMatchesRecord(rec, remote) {
		t.Fatalf("unchanged remote object should match its record")
	}
	if remoteMatchesRecord(nil, remote) {
		t.Fatalf("a missing record is never a match")
	}

	changedBlob := *rec
	changedBlob.RemoteBlobSHA1 = "blob2"
	if remoteMatchesRecord(&changedBlob, remote) {
		t.Fatalf("a different blob id must count as changed upstream")
	}

	changedSize := *rec
	changedSize.Size = 101
	if remoteMatchesRecord(&changedSize, remote) {
		t.Fatalf("a different size must count as changed upstream")
	}

	// The record's local mtime is irrelevant here: only the remote object is
	// being compared, which is what separates this from recordCurrent.
	touched := *rec
	touched.ModTimeUnixNano = 12345
	if !remoteMatchesRecord(&touched, remote) {
		t.Fatalf("local mtime must not affect the upstream comparison")
	}
}

func TestRemoteMatchesRecordLFS(t *testing.T) {
	remote := hub.RepoFile{Path: "w.safetensors", Size: 10, BlobID: "b", LFS: &hub.LFSInfo{SHA256: "aaa", Size: 10}}
	rec := &state.FileRecord{Path: "w.safetensors", Size: 10, RemoteBlobSHA1: "b", RemoteLFSSHA256: "aaa"}
	if !remoteMatchesRecord(rec, remote) {
		t.Fatalf("matching LFS object should match")
	}
	rotated := *rec
	rotated.RemoteLFSSHA256 = "bbb"
	if remoteMatchesRecord(&rotated, remote) {
		t.Fatalf("a different LFS sha256 must count as changed upstream")
	}
	// A record made before the file became LFS-tracked must not match.
	plain := &state.FileRecord{Path: "w.safetensors", Size: 10, RemoteBlobSHA1: "b"}
	if remoteMatchesRecord(plain, remote) {
		t.Fatalf("a record without the LFS hash must count as changed")
	}
}
