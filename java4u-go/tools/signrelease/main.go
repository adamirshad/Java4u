// Command signrelease makes and checks the signature on a Java4U release's checksums.txt
// (see internal/core/signing.go). Run it from the java4u-go folder:
//
//	go run ./tools/signrelease keygen <private-key-file>
//	    Makes a new key pair. Writes the private key (base64 seed) to the file, readable
//	    only by you, and prints the public key to put in core.ReleaseKeys.
//
//	JAVA4U_RELEASE_KEY=... go run ./tools/signrelease sign <tag> <checksums.txt>
//	    Writes <checksums.txt>.sig with one signature per key in JAVA4U_RELEASE_KEY and
//	    JAVA4U_RELEASE_KEY_NEXT (when set), then checks it against core.ReleaseKeys so a
//	    wrong or missing key fails here instead of on players' computers.
//
//	go run ./tools/signrelease verify <tag> <checksums.txt>
//	    Checks <checksums.txt>.sig against core.ReleaseKeys.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "signrelease:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: signrelease keygen <file> | sign <tag> <checksums.txt> | verify <tag> <checksums.txt>")
	}
	switch {
	case args[0] == "keygen" && len(args) == 2:
		return keygen(args[1])
	case args[0] == "sign" && len(args) == 3:
		return sign(args[1], args[2])
	case args[0] == "verify" && len(args) == 3:
		return verify(args[1], args[2])
	}
	return fmt.Errorf("unknown or incomplete command: %v", args)
}

func keygen(path string) error {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(tag, sumsPath string) error {
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	var out string
	for _, env := range []string{"JAVA4U_RELEASE_KEY", "JAVA4U_RELEASE_KEY_NEXT"} {
		v := os.Getenv(env)
		if v == "" {
			continue
		}
		key, err := core.ParseReleaseKey(v)
		if err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
		out += core.SignJava4URelease(key, tag, sums) + "\n"
	}
	if out == "" {
		return errors.New("JAVA4U_RELEASE_KEY is not set, so the release can't be signed")
	}
	sigPath := sumsPath + ".sig"
	if err := os.WriteFile(sigPath, []byte(out), 0o644); err != nil {
		return err
	}
	if err := core.VerifyJava4URelease(tag, sums, []byte(out)); err != nil {
		os.Remove(sigPath)
		return fmt.Errorf("signed, but not with a key in core.ReleaseKeys: %w", err)
	}
	fmt.Printf("signed %s for %s\n", sumsPath, tag)
	return nil
}

func verify(tag, sumsPath string) error {
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	sigs, err := os.ReadFile(sumsPath + ".sig")
	if err != nil {
		return err
	}
	if err := core.VerifyJava4URelease(tag, sums, sigs); err != nil {
		return err
	}
	fmt.Printf("%s is signed for %s\n", sumsPath, tag)
	return nil
}
