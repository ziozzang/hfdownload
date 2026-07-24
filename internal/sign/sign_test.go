package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	msg := []byte("# hftools SHA-256\nabc  model.safetensors\n")
	rec := Sign(msg, priv, "alice", time.Unix(1700000000, 0).UTC())
	if err := rec.Verify(msg, pub); err != nil {
		t.Fatalf("verify pinned: %v", err)
	}
	if err := rec.Verify(msg, nil); err != nil {
		t.Fatalf("verify unpinned: %v", err)
	}
}

func TestVerifyDetectsTamper(t *testing.T) {
	pub, priv, _ := GenerateKey()
	rec := Sign([]byte("original"), priv, "", time.Unix(1, 0))
	if err := rec.Verify([]byte("tampered"), pub); err == nil {
		t.Fatalf("expected verification failure on modified payload")
	}
}

func TestVerifyRejectsWrongPinnedKey(t *testing.T) {
	_, priv, _ := GenerateKey()
	other, _, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "", time.Unix(1, 0))
	if err := rec.Verify(msg, other); err == nil {
		t.Fatalf("expected rejection when pinned key differs")
	}
}

func TestPEMRoundTrip(t *testing.T) {
	_, priv, _ := GenerateKey()
	pemBytes, err := MarshalPrivateKeyPEM(priv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := ParsePrivateKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !got.Equal(priv) {
		t.Fatalf("private key round-trip mismatch")
	}
}

func TestParsePublicKeyHex(t *testing.T) {
	pub, _, _ := GenerateKey()
	parsed, err := ParsePublicKey(PublicKeyHex(pub))
	if err != nil {
		t.Fatalf("parse hex: %v", err)
	}
	if !parsed.Equal(pub) {
		t.Fatalf("public key hex round-trip mismatch")
	}
}

// A v2 signature covers the signer label, so rewriting it after signing must
// break verification — that is what makes the label evidence of who signed.
func TestVerifyDetectsSignerTamper(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "alice@corp.example", time.Unix(1700000000, 0).UTC())
	if rec.Version != 2 || !rec.MetadataSigned() {
		t.Fatalf("expected a v2 record, got version %d", rec.Version)
	}
	rec.Signer = "mallory@evil.example"
	if err := rec.Verify(msg, pub); err == nil {
		t.Fatalf("expected verification failure after rewriting the signer label")
	}
}

// The signing time is bound the same way, so a backdated record fails.
func TestVerifyDetectsTimestampTamper(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "alice", time.Unix(1700000000, 0).UTC())
	rec.SignedAt = rec.SignedAt.Add(-48 * time.Hour)
	if err := rec.Verify(msg, pub); err == nil {
		t.Fatalf("expected verification failure after backdating signed_at")
	}
}

// SignedAt must survive a JSON round-trip byte-for-byte, or stored signatures
// would fail to verify after being written to and read back from disk.
func TestVerifyAfterJSONRoundTrip(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "alice@corp.example", time.Now())
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded Record
	if err := json.Unmarshal(b, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := loaded.Verify(msg, pub); err != nil {
		t.Fatalf("verify after JSON round-trip: %v", err)
	}
}

// Signatures written by v1 binaries signed the payload alone; they must keep
// verifying, and must report that their labels are not signature-covered.
func TestVerifyLegacyV1Record(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	digest := sha256.Sum256(msg)
	legacy := Record{
		Version:      1,
		Algorithm:    "ed25519",
		Signer:       "alice",
		PublicKey:    PublicKeyHex(pub),
		Signature:    hex.EncodeToString(ed25519.Sign(priv, msg)),
		DigestSHA256: hex.EncodeToString(digest[:]),
		SignedAt:     time.Unix(1600000000, 0).UTC(),
	}
	if legacy.MetadataSigned() {
		t.Fatalf("v1 record must not claim its metadata is signed")
	}
	if err := legacy.Verify(msg, pub); err != nil {
		t.Fatalf("legacy v1 signature must still verify: %v", err)
	}
}

// Stripping the metadata signature from a v2 record must not silently downgrade
// it to an unauthenticated-label record that still verifies.
func TestVerifyRejectsStrippedMetadataSignature(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "alice@corp.example", time.Unix(1700000000, 0).UTC())
	rec.MetadataSignature = ""
	if rec.MetadataSigned() {
		t.Fatalf("a record without a metadata signature must not claim to have one")
	}
	if err := rec.Verify(msg, pub); err == nil {
		t.Fatalf("expected a v2 record missing its metadata signature to be rejected")
	}
}

// A v2 record's payload signature must remain verifiable by the v1 rule (raw
// payload), so binaries predating schema v2 keep accepting new signatures.
func TestV2PayloadSignatureStaysV1Compatible(t *testing.T) {
	pub, priv, _ := GenerateKey()
	msg := []byte("payload")
	rec := Sign(msg, priv, "alice", time.Unix(1700000000, 0).UTC())
	raw, err := hex.DecodeString(rec.Signature)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ed25519.Verify(pub, msg, raw) {
		t.Fatalf("payload signature must verify against the raw payload for older binaries")
	}
}
