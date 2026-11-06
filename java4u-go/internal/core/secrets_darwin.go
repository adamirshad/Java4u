package core

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// The macOS login Keychain, through Apple's own `security` tool.

type macSecrets struct{ tool string }

func platformSecrets() secretStore {
	p, err := exec.LookPath("security")
	if err != nil {
		return nil
	}
	return macSecrets{p}
}

func (macSecrets) Name() string { return "the macOS Keychain" }

func (m macSecrets) Get(key string) (string, error) {
	out, err := exec.Command(m.tool, "find-generic-password", "-s", keychainService, "-a", key, "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 { // errSecItemNotFound
			return "", errSecretNotFound
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (m macSecrets) Set(key, value string) error {
	// -U updates an existing item instead of failing.
	cmd := exec.Command(m.tool, "add-generic-password", "-U", "-s", keychainService, "-a", key, "-l", "Java4U account token", "-w", value)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New("saving to the Keychain: " + strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (m macSecrets) Delete(key string) error {
	_ = exec.Command(m.tool, "delete-generic-password", "-s", keychainService, "-a", key).Run()
	return nil
}
