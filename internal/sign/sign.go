// Package sign provides ed25519 provenance signatures over a repository's
// content digest. hftools already records a content-addressed SHA-256 manifest
// (.sha256) for every download; signing that file lets an air-gapped recipient
// confirm not only that the bytes are intact (which hashing alone proves) but
// that they came from a holder of a specific private key — a trust chain that
// survives transfer over untrusted media.
package sign

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RecordVersion is the on-disk signature schema version. Version 2 binds the
// signer label and the signing time into the signed bytes; version 1 signed the
// manifest payload alone, which left those two fields as unauthenticated
// annotations anyone could rewrite without breaking verification.
const RecordVersion = 2

// Record is the detached signature stored alongside a repository.
type Record struct {
	Version      int       `json:"version"`
	Algorithm    string    `json:"algorithm"`
	Signer       string    `json:"signer,omitempty"`
	PublicKey    string    `json:"public_key"`    // hex-encoded 32-byte ed25519 public key
	Signature    string    `json:"signature"`     // hex-encoded 64-byte signature over the payload
	DigestSHA256 string    `json:"digest_sha256"` // hex sha256 of the signed payload
	SignedAt     time.Time `json:"signed_at"`
	// MetadataSignature covers SignedPreimage: the signer label and signing
	// time together with the payload digest. It is a second signature rather
	// than a change to Signature so that binaries predating schema v2 — which
	// verify Signature against the payload alone — still accept these records.
	// That matters for air-gapped recipients who cannot upgrade on demand.
	MetadataSignature string `json:"metadata_signature,omitempty"`
}

// GenerateKey returns a fresh ed25519 keypair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// MarshalPrivateKeyPEM encodes a private key as a PKCS#8 PEM block.
func MarshalPrivateKeyPEM(priv ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParsePrivateKeyPEM decodes a PKCS#8 PEM ed25519 private key.
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an ed25519 private key")
	}
	return priv, nil
}

// PublicKeyHex renders a public key as hex.
func PublicKeyHex(pub ed25519.PublicKey) string { return hex.EncodeToString(pub) }

// MarshalPublicKeyPEM encodes a public key as a PKIX PEM block, the portable
// form recipients pin out-of-band.
func MarshalPublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// Fingerprint is the SHA-256 of the raw 32-byte public key, hex-encoded. It is
// the stable, human-comparable identifier used to pin trust in a signer.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint returns the first 16 hex chars of the fingerprint for compact
// display; the full fingerprint remains the value to compare for trust.
func ShortFingerprint(pub ed25519.PublicKey) string {
	fp := Fingerprint(pub)
	if len(fp) > 16 {
		return fp[:16]
	}
	return fp
}

// ParsePublicKey accepts a hex-encoded key or a PEM/PKIX block.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "BEGIN") {
		block, _ := pem.Decode([]byte(s))
		if block == nil {
			return nil, fmt.Errorf("no PEM block found")
		}
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		pub, ok := key.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not an ed25519 public key")
		}
		return pub, nil
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("public key is not valid hex or PEM: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ed25519 public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// SignedPreimage returns the exact bytes a version-2 signature covers: a
// domain-separated header, the signer label, the signing time, and the digest of
// the manifest payload. Binding the label and time means neither can be edited
// after the fact without invalidating the signature — which is what turns "who
// signed this, and when" into evidence rather than an annotation. The signer is
// quoted so a label containing newlines cannot be confused with another field.
func SignedPreimage(payload []byte, signer string, signedAt time.Time) []byte {
	digest := sha256.Sum256(payload)
	var b strings.Builder
	b.WriteString("hftools-signature-v2\n")
	b.WriteString("algorithm: ed25519\n")
	b.WriteString("signer: " + strconv.Quote(signer) + "\n")
	b.WriteString("signed_at: " + signedAt.UTC().Format(time.RFC3339Nano) + "\n")
	b.WriteString("payload_sha256: " + hex.EncodeToString(digest[:]) + "\n")
	return []byte(b.String())
}

// MetadataSigned reports whether the signer label and signing time are covered
// by a signature. Version 1 records carry them unauthenticated, so callers must
// not present them as proof of who signed a repository.
func (r Record) MetadataSigned() bool { return r.Version >= 2 && r.MetadataSignature != "" }

// Sign produces a Record over message using priv. It emits two signatures: one
// over the payload (which any hftools version can check) and one over the
// preimage binding the signer label and timestamp.
func Sign(message []byte, priv ed25519.PrivateKey, signer string, now time.Time) Record {
	now = now.UTC()
	digest := sha256.Sum256(message)
	return Record{
		Version:           RecordVersion,
		Algorithm:         "ed25519",
		Signer:            signer,
		PublicKey:         PublicKeyHex(priv.Public().(ed25519.PublicKey)),
		Signature:         hex.EncodeToString(ed25519.Sign(priv, message)),
		MetadataSignature: hex.EncodeToString(ed25519.Sign(priv, SignedPreimage(message, signer, now))),
		DigestSHA256:      hex.EncodeToString(digest[:]),
		SignedAt:          now,
	}
}

// Verify checks the record against message. When pinned is non-nil the record's
// embedded key must equal it (provenance); otherwise only tamper-evidence
// against the embedded key is proven, which the caller should flag.
func (r Record) Verify(message []byte, pinned ed25519.PublicKey) error {
	if r.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", r.Algorithm)
	}
	pub, err := ParsePublicKey(r.PublicKey)
	if err != nil {
		return fmt.Errorf("record public key: %w", err)
	}
	if pinned != nil && !pub.Equal(pinned) {
		return fmt.Errorf("signature key %s does not match the pinned key", r.PublicKey)
	}
	sig, err := hex.DecodeString(r.Signature)
	if err != nil {
		return fmt.Errorf("signature is not valid hex: %w", err)
	}
	digest := sha256.Sum256(message)
	if hex.EncodeToString(digest[:]) != r.DigestSHA256 {
		return fmt.Errorf("signed payload digest mismatch: content changed since signing")
	}
	if !ed25519.Verify(pub, message, sig) {
		return fmt.Errorf("signature verification failed")
	}
	// A v2 record must also carry a valid signature over its signer label and
	// timestamp. Refusing a v2 record whose metadata signature is missing or
	// wrong is what stops those fields from being rewritten in place.
	if r.Version >= 2 {
		if r.MetadataSignature == "" {
			return fmt.Errorf("v%d signature is missing its metadata signature (signer and time cannot be trusted)", r.Version)
		}
		metaSig, err := hex.DecodeString(r.MetadataSignature)
		if err != nil {
			return fmt.Errorf("metadata signature is not valid hex: %w", err)
		}
		if !ed25519.Verify(pub, SignedPreimage(message, r.Signer, r.SignedAt), metaSig) {
			return fmt.Errorf("metadata signature verification failed (the signer label or signing time may have been altered)")
		}
	}
	return nil
}
