package core

// Persistent Microsoft accounts and the login/refresh orchestration.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StoredAccount struct {
	Username      string `json:"username"`
	UUID          string `json:"uuid"`
	RefreshToken  string `json:"refresh_token"`
	MCAccessToken string `json:"mc_access_token"`
	// MCExpiresAt is in Unix seconds.
	MCExpiresAt int64 `json:"mc_expires_at"`
}

type AccountStore struct {
	ClientID string          `json:"client_id,omitempty"`
	Accounts []StoredAccount `json:"accounts"`
}

func now() int64 { return time.Now().Unix() }

// writePrivate writes a file readable only by the current user (on Unix), via temp file + rename.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadAccounts(dirs Dirs) (*AccountStore, error) {
	s := &AccountStore{}
	data, err := os.ReadFile(dirs.AccountsFile())
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", dirs.AccountsFile(), err)
	}
	return s, nil
}

func (s *AccountStore) Save(dirs Dirs) error {
	if s.Accounts == nil {
		s.Accounts = []StoredAccount{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(dirs.AccountsFile(), data)
}

// Find looks up by username (case-insensitive); with an empty name, the first account.
func (s *AccountStore) Find(name string) *StoredAccount {
	if name == "" {
		if len(s.Accounts) == 0 {
			return nil
		}
		return &s.Accounts[0]
	}
	for i := range s.Accounts {
		if strings.EqualFold(s.Accounts[i].Username, name) {
			return &s.Accounts[i]
		}
	}
	return nil
}

func (s *AccountStore) Remove(name string) bool {
	kept := s.Accounts[:0]
	for _, a := range s.Accounts {
		if !strings.EqualFold(a.Username, name) {
			kept = append(kept, a)
		}
	}
	removed := len(kept) != len(s.Accounts)
	s.Accounts = kept
	return removed
}

func (s *AccountStore) upsert(acct StoredAccount) {
	for i := range s.Accounts {
		if s.Accounts[i].UUID == acct.UUID {
			s.Accounts[i] = acct
			return
		}
	}
	s.Accounts = append(s.Accounts, acct)
}

func storedFrom(sess *MCSession, refreshToken string) StoredAccount {
	return StoredAccount{
		Username:      sess.Username,
		UUID:          sess.UUID,
		RefreshToken:  refreshToken,
		MCAccessToken: sess.AccessToken,
		MCExpiresAt:   now() + sess.ExpiresIn,
	}
}

func (s StoredAccount) Account() Account {
	return Account{Username: s.Username, UUID: s.UUID, AccessToken: s.MCAccessToken, UserType: "msa"}
}

// Login runs the interactive device-code login. show is called once with the code to display.
func Login(ctx context.Context, c *http.Client, dirs Dirs, clientID string, show func(*DeviceCode)) (StoredAccount, error) {
	dc, err := RequestDeviceCode(ctx, c, clientID)
	if err != nil {
		return StoredAccount{}, err
	}
	show(dc)
	tokens, err := PollForToken(ctx, c, clientID, dc)
	if err != nil {
		return StoredAccount{}, err
	}
	sess, err := MinecraftLogin(ctx, c, tokens.AccessToken)
	if err != nil {
		return StoredAccount{}, err
	}
	acct := storedFrom(sess, tokens.RefreshToken)

	store, err := LoadAccounts(dirs)
	if err != nil {
		return StoredAccount{}, err
	}
	store.ClientID = clientID
	store.upsert(acct)
	if err := store.Save(dirs); err != nil {
		return StoredAccount{}, err
	}
	return acct, nil
}

// AccountForLaunch returns a launch-ready account, refreshing the stored session if it is
// about to expire. An empty name means the first saved account.
func AccountForLaunch(ctx context.Context, c *http.Client, dirs Dirs, name, clientIDOverride string) (Account, error) {
	store, err := LoadAccounts(dirs)
	if err != nil {
		return Account{}, err
	}
	found := store.Find(name)
	if found == nil {
		if name != "" {
			return Account{}, fmt.Errorf("no saved account named '%s'; run `goose account login`", name)
		}
		return Account{}, errors.New("no saved accounts; run `goose account login` (or use --offline NAME for testing)")
	}
	stored := *found

	if stored.MCExpiresAt > now()+120 {
		return stored.Account(), nil
	}

	clientID := clientIDOverride
	if clientID == "" {
		clientID = store.ClientID
	}
	if clientID == "" {
		return Account{}, errors.New("no Azure client id known; pass --client-id")
	}
	tokens, err := RefreshTokens(ctx, c, clientID, stored.RefreshToken)
	if err != nil {
		return Account{}, err
	}
	sess, err := MinecraftLogin(ctx, c, tokens.AccessToken)
	if err != nil {
		return Account{}, err
	}
	fresh := storedFrom(sess, tokens.RefreshToken)
	store.upsert(fresh)
	if err := store.Save(dirs); err != nil {
		return Account{}, err
	}
	return fresh.Account(), nil
}
