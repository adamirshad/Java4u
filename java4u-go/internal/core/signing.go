package core

// Signed release checksums, used for Java4U's own updates and for Horizon's launcher.
//
// A release carries a checksum list (sha256sum format) and a signature file: one base64
// Ed25519 signature per line, each over "<project> release <tag>\n" followed by the list's
// bytes. Naming the tag means a list signed for one version can't be passed off as another.

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ReleaseKeys are the Ed25519 public keys (base64) Java4U's releases are signed with. The
// private key is the JAVA4U_RELEASE_KEY secret of the GitHub repository (see the README,
// "Releasing"). To change keys, ship a release that trusts both the old and new key before
// signing with the new one. Tests change it.
var ReleaseKeys = []string{"Omg9uTPtHBZadDfafFHuYBafK5AqpGzT4IpvYdnEHe8="}

// ReleaseSigName is the signature file published next to checksums.txt.
const ReleaseSigName = "checksums.txt.sig"

func releaseMessage(project, tag string, sums []byte) []byte {
	return append([]byte(project+" release "+tag+"\n"), sums...)
}

// verifyReleaseSigs reports whether any signature line in sigs is a valid signature by one
// of keys over project's release message for tag and sums.
func verifyReleaseSigs(project, tag string, sums, sigs []byte, keys []string) bool {
	msg := releaseMessage(project, tag, sums)
	for _, k := range keys {
		key, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		for _, line := range strings.Fields(string(sigs)) {
			sig, err := base64.StdEncoding.DecodeString(line)
			if err == nil && ed25519.Verify(key, msg, sig) {
				return true
			}
		}
	}
	return false
}

// VerifyJava4URelease checks that sigs holds a signature by one of ReleaseKeys over the
// checksum list sums for release tag.
func VerifyJava4URelease(tag string, sums, sigs []byte) error {
	if !verifyReleaseSigs("java4u", tag, sums, sigs, ReleaseKeys) {
		return fmt.Errorf("Java4U %s isn't signed with Java4U's release key, so it was not installed", tag)
	}
	return nil
}

// SignJava4URelease returns one signature line for the checksum list sums of release tag.
func SignJava4URelease(key ed25519.PrivateKey, tag string, sums []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, releaseMessage("java4u", tag, sums)))
}

// ParseReleaseKey reads a private key written by `signrelease keygen`: the base64 32-byte
// seed. Surrounding spaces and newlines are ignored.
func ParseReleaseKey(s string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("not a base64 Ed25519 seed (32 bytes)")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
