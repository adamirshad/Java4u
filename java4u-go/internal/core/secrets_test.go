package core

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Tests never touch the real keychain unless a test asks for it.
func TestMain(m *testing.M) {
	secrets = memorySecrets{}
	os.Exit(m.Run())
}

func TestTokensGoToKeychain(t *testing.T) {
	mem := memorySecrets{}
	secrets = mem
	defer func() { secrets = memorySecrets{} }()

	dirs := Dirs{Root: t.TempDir()}
	s := &AccountStore{Accounts: []StoredAccount{{Username: "Goose", UUID: "u1", RefreshToken: "refresh", MCAccessToken: "access", MCExpiresAt: 9}}}
	if err := s.Save(dirs); err != nil {
		t.Fatal(err)
	}
	file, _ := os.ReadFile(dirs.AccountsFile())
	if strings.Contains(string(file), "refresh") || strings.Contains(string(file), "access\"") {
		t.Fatalf("tokens leaked into the file: %s", file)
	}
	if _, ok := mem[secretKey("u1")]; !ok {
		t.Fatal("tokens not in keychain")
	}
	back, err := LoadAccounts(dirs)
	if err != nil || back.Accounts[0].RefreshToken != "refresh" || back.Accounts[0].MCAccessToken != "access" {
		t.Fatalf("load: %+v %v", back, err)
	}
	if !back.Remove("goose") || len(mem) != 0 {
		t.Fatal("remove should delete the keychain entry")
	}
}

func TestOldTokensMigrate(t *testing.T) {
	mem := memorySecrets{}
	secrets = mem
	defer func() { secrets = memorySecrets{} }()

	dirs := Dirs{Root: t.TempDir()}
	old, _ := json.Marshal(map[string]any{"accounts": []map[string]any{{"username": "A", "uuid": "u", "refresh_token": "r", "mc_access_token": "t", "mc_expires_at": 5}}})
	os.MkdirAll(dirs.Root, 0o755)
	os.WriteFile(dirs.AccountsFile(), old, 0o600)
	s, err := LoadAccounts(dirs)
	if err != nil || s.Accounts[0].RefreshToken != "r" {
		t.Fatalf("%+v %v", s, err)
	}
	file, _ := os.ReadFile(dirs.AccountsFile())
	if strings.Contains(string(file), `"r"`) || !strings.Contains(string(file), "in_keychain") {
		t.Fatalf("not migrated: %s", file)
	}
}

func TestNoKeychainKeepsFile(t *testing.T) {
	secrets = nil
	defer func() { secrets = memorySecrets{} }()
	dirs := Dirs{Root: t.TempDir()}
	s := &AccountStore{Accounts: []StoredAccount{{Username: "A", UUID: "u", RefreshToken: "r"}}}
	s.Save(dirs)
	back, _ := LoadAccounts(dirs)
	if back.Accounts[0].RefreshToken != "r" {
		t.Fatal("file fallback lost the token")
	}
}

// The real Windows Credential Manager, including a token longer than one credential blob.
func TestWindowsCredentialManager(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	store := platformSecrets()
	if store == nil {
		t.Skip("no Credential Manager")
	}
	key := "test/" + t.Name()
	defer store.Delete(key)
	long := strings.Repeat("x", 2560*2+17)
	for _, v := range []string{long, "short"} {
		if err := store.Set(key, v); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get(key)
		if err != nil || got != v {
			t.Fatalf("got %d chars, want %d (%v)", len(got), len(v), err)
		}
	}
	store.Delete(key)
	if _, err := store.Get(key); err == nil {
		t.Fatal("still there after delete")
	}
}
