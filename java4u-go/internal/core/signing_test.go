package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestReleaseKeysAreValid(t *testing.T) {
	if len(ReleaseKeys) == 0 {
		t.Fatal("no release keys: every update would be refused")
	}
	for _, k := range ReleaseKeys {
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) != ed25519.PublicKeySize {
			t.Fatalf("%q is not an Ed25519 public key", k)
		}
	}
}

func TestSignAndVerifyRelease(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	old := ReleaseKeys
	ReleaseKeys = []string{"not base64!", base64.StdEncoding.EncodeToString(pub)}
	defer func() { ReleaseKeys = old }()

	key, err := ParseReleaseKey("  " + base64.StdEncoding.EncodeToString(priv.Seed()) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc  java4u-linux-amd64.zip\n")
	sig := SignJava4URelease(key, "v1.0.0", sums)
	if err := VerifyJava4URelease("v1.0.0", sums, []byte(sig)); err != nil {
		t.Fatalf("good signature refused: %v", err)
	}
	if VerifyJava4URelease("v1.0.1", sums, []byte(sig)) == nil {
		t.Fatal("signature for another version accepted")
	}
	if VerifyJava4URelease("v1.0.0", append(sums, 'x'), []byte(sig)) == nil {
		t.Fatal("changed list accepted")
	}
	// A Horizon signature over the same list must not pass for Java4U.
	horizon := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, releaseMessage("horizon", "v1.0.0", sums)))
	if VerifyJava4URelease("v1.0.0", sums, []byte(horizon)) == nil {
		t.Fatal("Horizon-style signature accepted for Java4U")
	}
	if _, err := ParseReleaseKey("short"); err == nil {
		t.Fatal("bad private key accepted")
	}
}
