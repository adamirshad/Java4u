package core

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
)

// Account is what the game needs to start: a name, a UUID and an access token.
type Account struct {
	Username string `json:"username"`
	// UUID is the 32-char hex UUID without dashes.
	UUID        string `json:"uuid"`
	AccessToken string `json:"access_token"`
	// UserType is "msa" for Microsoft accounts, "legacy" for offline.
	UserType string `json:"user_type"`
}

// ValidUsername reports whether name is a legal Minecraft username.
func ValidUsername(name string) bool {
	if len(name) < 3 || len(name) > 16 {
		return false
	}
	for _, c := range name {
		ok := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// OfflineAccount uses the same UUID the vanilla server derives for offline mode.
// For development and testing only.
func OfflineAccount(name string) (Account, error) {
	if !ValidUsername(name) {
		return Account{}, errors.New("username must be 3-16 characters: letters, digits or underscore")
	}
	return Account{Username: name, UUID: OfflineUUID(name), AccessToken: "0", UserType: "legacy"}, nil
}

// OfflineUUID is Java's UUID.nameUUIDFromBytes("OfflinePlayer:<name>").
func OfflineUUID(name string) string {
	b := md5.Sum([]byte("OfflinePlayer:" + name))
	b[6] = (b[6] & 0x0f) | 0x30
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}
