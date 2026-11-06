package core

// Secret storage for account tokens: the OS keychain where there is one (Windows
// Credential Manager, the macOS Keychain, the Linux Secret Service), so tokens never sit
// in a plain file.

import "errors"

// keychainService names Java4U's entries in the OS keychain.
const keychainService = "Java4U"

// errSecretNotFound means the keychain has no entry for the key.
var errSecretNotFound = errors.New("secret not found")

type secretStore interface {
	// Name is shown to users, e.g. "Windows Credential Manager".
	Name() string
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// secrets is the keychain in use, or nil when there is none. Tests replace it.
var secrets = platformSecrets()

// KeychainName says where account tokens are kept, for display.
func KeychainName() string {
	if secrets == nil {
		return "a private file in the data folder"
	}
	return secrets.Name()
}

// memorySecrets is an in-memory secretStore for tests.
type memorySecrets map[string]string

func (m memorySecrets) Name() string { return "memory" }

func (m memorySecrets) Get(key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", errSecretNotFound
	}
	return v, nil
}

func (m memorySecrets) Set(key, value string) error {
	m[key] = value
	return nil
}

func (m memorySecrets) Delete(key string) error {
	delete(m, key)
	return nil
}
