package core

// Microsoft -> Xbox Live -> XSTS -> Minecraft services sign-in (device code flow).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deviceCodeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	tokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	msScope       = "XboxLive.signin offline_access"
	xblURL        = "https://user.auth.xboxlive.com/user/authenticate"
	xstsURL       = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL    = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL  = "https://api.minecraftservices.com/minecraft/profile"
)

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
}

type MSTokens struct {
	AccessToken  string
	RefreshToken string
}

type MCSession struct {
	AccessToken string
	ExpiresIn   int64
	UUID        string
	Username    string
}

type tokenResp struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func postForm(ctx context.Context, c *http.Client, u string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.Do(req)
}

func postFormToken(ctx context.Context, c *http.Client, form url.Values) (tokenResp, error) {
	var tr tokenResp
	resp, err := postForm(ctx, c, tokenURL, form)
	if err != nil {
		return tr, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&tr)
	return tr, err
}

// RequestDeviceCode asks Microsoft for a code the user types in at microsoft.com/link.
func RequestDeviceCode(ctx context.Context, c *http.Client, clientID string) (*DeviceCode, error) {
	resp, err := postForm(ctx, c, deviceCodeURL, url.Values{"client_id": {clientID}, "scope": {msScope}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("device code request failed (%s). Check your Azure client id and that "+
			"'Allow public client flows' is enabled: %s", resp.Status, body)
	}
	dc := DeviceCode{Interval: 5}
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	return &dc, nil
}

func tokensFrom(r tokenResp) (MSTokens, error) {
	if r.AccessToken == "" || r.RefreshToken == "" {
		return MSTokens{}, errors.New("Microsoft returned no tokens")
	}
	return MSTokens{AccessToken: r.AccessToken, RefreshToken: r.RefreshToken}, nil
}

var errCodeExpired = errors.New("the login code expired; run login again")

// PollForToken waits until the user finishes signing in (or the code expires).
func PollForToken(ctx context.Context, c *http.Client, clientID string, dc *DeviceCode) (MSTokens, error) {
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	interval := max(dc.Interval, 1)
	for {
		select {
		case <-time.After(time.Duration(interval) * time.Second):
		case <-ctx.Done():
			return MSTokens{}, ctx.Err()
		}
		if time.Now().After(deadline) {
			return MSTokens{}, errCodeExpired
		}
		r, err := postFormToken(ctx, c, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
		})
		if err != nil {
			return MSTokens{}, err
		}
		if r.AccessToken != "" {
			return tokensFrom(r)
		}
		switch r.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5
		case "authorization_declined", "access_denied":
			return MSTokens{}, errors.New("sign-in was declined")
		case "expired_token":
			return MSTokens{}, errCodeExpired
		default:
			e := r.Error
			if e == "" {
				e = "unknown error"
			}
			return MSTokens{}, fmt.Errorf("Microsoft sign-in failed: %s %s", e, r.ErrorDescription)
		}
	}
}

// RefreshTokens exchanges a refresh token for fresh Microsoft tokens (the refresh token rotates).
func RefreshTokens(ctx context.Context, c *http.Client, clientID, refreshToken string) (MSTokens, error) {
	r, err := postFormToken(ctx, c, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"scope":         {msScope},
	})
	if err != nil {
		return MSTokens{}, err
	}
	if r.AccessToken == "" {
		why := r.ErrorDescription
		if why == "" {
			why = r.Error
		}
		return MSTokens{}, fmt.Errorf("saved login is no longer valid, run `goose account login` again (%s)", why)
	}
	return tokensFrom(r)
}

// postJSON sends body as JSON and decodes the reply into out (ignoring decode errors,
// since error replies are often empty). It returns the HTTP status.
func postJSON(ctx context.Context, c *http.Client, u string, body, out any) (int, string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(out)
	return resp.StatusCode, resp.Status, nil
}

func xstsError(code uint64) string {
	switch code {
	case 2148916233:
		return "this Microsoft account has no Xbox profile; sign in once at minecraft.net or xbox.com to create one"
	case 2148916235:
		return "Xbox Live is not available in your country/region"
	case 2148916236, 2148916237:
		return "this account needs adult verification on Xbox"
	case 2148916238:
		return "this is a child account; it must be added to a Microsoft Family by an adult"
	}
	return "Xbox Live rejected the sign-in"
}

// MinecraftLogin turns a Microsoft access token into a Minecraft access token + profile.
func MinecraftLogin(ctx context.Context, c *http.Client, msAccessToken string) (*MCSession, error) {
	// Xbox Live
	var xbl struct {
		Token         string
		DisplayClaims struct {
			Xui []struct {
				Uhs string `json:"uhs"`
			} `json:"xui"`
		}
	}
	code, status, err := postJSON(ctx, c, xblURL, map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + msAccessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}, &xbl)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("Xbox Live authentication failed (%s)", status)
	}
	if xbl.Token == "" {
		return nil, errors.New("Xbox Live response had no token")
	}
	if len(xbl.DisplayClaims.Xui) == 0 || xbl.DisplayClaims.Xui[0].Uhs == "" {
		return nil, errors.New("Xbox Live response had no user hash")
	}
	uhs := xbl.DisplayClaims.Xui[0].Uhs

	// XSTS
	var xsts struct {
		Token string
		XErr  uint64
	}
	code, status, err = postJSON(ctx, c, xstsURL, map[string]any{
		"Properties":   map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{xbl.Token}},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}, &xsts)
	if err != nil {
		return nil, err
	}
	if code/100 != 2 {
		msg := "XSTS authorization failed"
		if xsts.XErr != 0 {
			msg = xstsError(xsts.XErr)
		}
		return nil, fmt.Errorf("%s (%s)", msg, status)
	}
	if xsts.Token == "" {
		return nil, errors.New("XSTS response had no token")
	}

	// Minecraft services
	var mc struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	code, status, err = postJSON(ctx, c, mcLoginURL, map[string]any{
		"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xsts.Token),
	}, &mc)
	if err != nil {
		return nil, err
	}
	if code == http.StatusForbidden {
		return nil, errors.New("Minecraft services refused the login (403). Your Azure app probably has not been " +
			"approved for the Minecraft API yet; see the README")
	}
	if code/100 != 2 {
		return nil, fmt.Errorf("Minecraft login failed (%s)", status)
	}
	if mc.AccessToken == "" {
		return nil, errors.New("Minecraft login response had no token")
	}
	if mc.ExpiresIn == 0 {
		mc.ExpiresIn = 86400
	}

	// Profile (404 means the account does not own Java Edition)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcProfileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+mc.AccessToken)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("this Microsoft account does not own Minecraft: Java Edition")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fetching the Minecraft profile failed (%s)", resp.Status)
	}
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}
	if profile.ID == "" {
		return nil, errors.New("profile had no id")
	}
	if profile.Name == "" {
		return nil, errors.New("profile had no name")
	}
	return &MCSession{AccessToken: mc.AccessToken, ExpiresIn: mc.ExpiresIn, UUID: profile.ID, Username: profile.Name}, nil
}
