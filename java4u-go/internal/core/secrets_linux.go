package core

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// The Secret Service (GNOME Keyring, KWallet) through libsecret's `secret-tool`. Without
// it, tokens stay in the private accounts file.

type linuxSecrets struct{ tool string }

func platformSecrets() secretStore {
	p, err := exec.LookPath("secret-tool")
	if err != nil {
		return nil
	}
	// A desktop session must be running for the keyring to answer.
	if err := exec.Command(p, "search", "service", keychainService).Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil
		}
	}
	return linuxSecrets{p}
}

func (linuxSecrets) Name() string { return "the system keyring" }

func (l linuxSecrets) Get(key string) (string, error) {
	out, err := exec.Command(l.tool, "lookup", "service", keychainService, "account", key).Output()
	if err != nil || len(out) == 0 {
		return "", errSecretNotFound
	}
	return string(out), nil
}

func (l linuxSecrets) Set(key, value string) error {
	cmd := exec.Command(l.tool, "store", "--label=Java4U account token", "service", keychainService, "account", key)
	cmd.Stdin = strings.NewReader(value) // never on the command line
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New("saving to the keyring: " + strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (l linuxSecrets) Delete(key string) error {
	_ = exec.Command(l.tool, "clear", "service", keychainService, "account", key).Run()
	return nil
}
