package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ziozzang/hftools/internal/sign"
)

// TestSignVerifyRoundTripWithHomeIdentity drives the whole home-identity flow:
// `sign` auto-creates ~/.hftools, `verify-sig` proves integrity unpinned, then
// recognizes provenance once the key is trusted, and rejects a tampered manifest
// and a mismatched pinned key.
func TestSignVerifyRoundTripWithHomeIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HFTOOLS_HOME", home)

	repo := t.TempDir()
	manifest := "# hftools SHA-256\nabc123  model.safetensors\n"
	if err := os.WriteFile(filepath.Join(repo, ".sha256"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// Signing with no --key creates the home identity on first use.
	if err := signCommand([]string{"--output", repo, "--signer", "jung@jioh.net"}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	for _, name := range []string{"signing.key", "signing.pub", "config.yaml"} {
		if _, err := os.Stat(filepath.Join(home, name)); err != nil {
			t.Fatalf("expected %s in home: %v", name, err)
		}
	}

	// Verify without a pin: integrity holds, provenance is unproven (no error).
	if err := verifySigCommand([]string{"--output", repo}); err != nil {
		t.Fatalf("verify unpinned: %v", err)
	}

	// Trust our own public key by file, then verify auto-recognizes it.
	if err := keyCommand([]string{"trust", "self", filepath.Join(home, "signing.pub")}); err != nil {
		t.Fatalf("key trust: %v", err)
	}
	if err := verifySigCommand([]string{"--output", repo}); err != nil {
		t.Fatalf("verify trusted: %v", err)
	}
	// And by trusted name via --pubkey.
	if err := verifySigCommand([]string{"--output", repo, "--pubkey", "self"}); err != nil {
		t.Fatalf("verify by trusted name: %v", err)
	}

	// A mismatched pinned key must fail.
	otherPub, _, _ := sign.GenerateKey()
	if err := verifySigCommand([]string{"--output", repo, "--pubkey", sign.PublicKeyHex(otherPub)}); err == nil {
		t.Fatalf("expected failure with mismatched pinned key")
	}

	// Tampering with the manifest must fail verification.
	if err := os.WriteFile(filepath.Join(repo, ".sha256"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySigCommand([]string{"--output", repo}); err == nil {
		t.Fatalf("expected failure after manifest tamper")
	}
}

// TestAutoSignRepoHook exercises the shared hook behind --sign / auto_sign:
// it creates the home identity on first use, signs a repo's .sha256, and the
// resulting signature verifies.
func TestAutoSignRepoHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HFTOOLS_HOME", home)

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".sha256"), []byte("# hftools SHA-256\nabc  model.bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(repo, ".metadata")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := autoSignRepo(repo, stateDir); err != nil {
		t.Fatalf("autoSignRepo: %v", err)
	}
	for _, p := range []string{filepath.Join(stateDir, "signature.json"), filepath.Join(repo, ".sha256.sig"), filepath.Join(home, "signing.key")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
	}
	if err := verifySigCommand([]string{"--output", repo}); err != nil {
		t.Fatalf("verify-sig after auto-sign: %v", err)
	}

	// config.yaml auto_sign toggled by `key init --auto-sign` drives homeAutoSign.
	t.Setenv("HFTOOLS_HOME", t.TempDir())
	if homeAutoSign() {
		t.Fatalf("auto-sign should be off before init")
	}
	if err := keyCommand([]string{"init", "--auto-sign"}); err != nil {
		t.Fatalf("key init --auto-sign: %v", err)
	}
	if !homeAutoSign() {
		t.Fatalf("auto-sign should be on after `key init --auto-sign`")
	}
}

func TestKeyInitRefusesOverwriteWithoutForce(t *testing.T) {
	t.Setenv("HFTOOLS_HOME", t.TempDir())
	if err := keyCommand([]string{"init", "--signer", "a@b.c"}); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := keyCommand([]string{"init"}); err == nil {
		t.Fatalf("expected refusal to overwrite existing identity")
	}
	if err := keyCommand([]string{"init", "--force"}); err != nil {
		t.Fatalf("init --force: %v", err)
	}
}
