//go:build !windows && !darwin && !linux

package core

// No keychain integration on this system; tokens stay in the private accounts file.
func platformSecrets() secretStore { return nil }
