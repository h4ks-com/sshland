package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// LogtoConfig holds the OAuth 2.0 / OIDC parameters for a Logto deployment.
type LogtoConfig struct {
	Endpoint      string // e.g. https://auth.h4ks.com
	AppID         string
	AppSecret     string
	RedirectURI   string
	IdentitiesDir string
}

// logtoConfigFromEnv returns nil when LOGTO_ENDPOINT is unset, falling back to SSH-key-based nick registration.
func logtoConfigFromEnv(identitiesDir string) *LogtoConfig {
	endpoint := os.Getenv("LOGTO_ENDPOINT")
	if endpoint == "" {
		return nil
	}
	return &LogtoConfig{
		Endpoint:      endpoint,
		AppID:         os.Getenv("LOGTO_APP_ID"),
		AppSecret:     os.Getenv("LOGTO_APP_SECRET"),
		RedirectURI:   os.Getenv("LOGTO_REDIRECT_URI"),
		IdentitiesDir: identitiesDir,
	}
}

// BuildAuthURL constructs the Logto authorization URL for the given state token.
func (c *LogtoConfig) BuildAuthURL(state string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", c.AppID)
	v.Set("redirect_uri", c.RedirectURI)
	v.Set("scope", "openid profile")
	v.Set("state", state)
	return c.Endpoint + "/oidc/auth?" + v.Encode()
}

// exchangeCodeForUserInfo exchanges an authorization code for the user's sub
// and username by calling /oidc/token then /oidc/me.
func (c *LogtoConfig) exchangeCodeForUserInfo(code string) (sub, username string, err error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	form.Set("client_id", c.AppID)
	form.Set("client_secret", c.AppSecret)

	resp, err := http.PostForm(c.Endpoint+"/oidc/token", form)
	if err != nil {
		return "", "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", "", fmt.Errorf("parsing token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", "", fmt.Errorf("token error: %s", tokenResp.Error)
	}

	req, err := http.NewRequest(http.MethodGet, c.Endpoint+"/oidc/me", nil)
	if err != nil {
		return "", "", fmt.Errorf("building userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	meResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("userinfo request: %w", err)
	}
	defer func() { _ = meResp.Body.Close() }()
	meBody, err := io.ReadAll(meResp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading userinfo response: %w", err)
	}
	if meResp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo endpoint %d: %s", meResp.StatusCode, meBody)
	}

	var userInfo struct {
		Sub      string `json:"sub"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(meBody, &userInfo); err != nil {
		return "", "", fmt.Errorf("parsing userinfo: %w", err)
	}
	return userInfo.Sub, userInfo.Username, nil
}

// AuthResult is the outcome of a completed OAuth flow.
type AuthResult struct {
	Username string
	Err      error
}

// PendingAuth tracks an in-flight login attempt keyed by OAuth state token.
type PendingAuth struct {
	PublicKey gossh.PublicKey
	ResultCh  chan AuthResult
	ExpiresAt time.Time
}

// PendingAuthManager coordinates concurrent login attempts.
type PendingAuthManager struct {
	mu      sync.Mutex
	pending map[string]*PendingAuth
	started bool
}

// Register stores a pending auth entry and returns the channel on which the
// result will be delivered. The first call also starts the expiry cleaner.
func (m *PendingAuthManager) Register(state string, key gossh.PublicKey) chan AuthResult {
	ch := make(chan AuthResult, 1)
	m.mu.Lock()
	if m.pending == nil {
		m.pending = make(map[string]*PendingAuth)
	}
	if !m.started {
		m.started = true
		go m.cleanupLoop()
	}
	m.pending[state] = &PendingAuth{
		PublicKey: key,
		ResultCh:  ch,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	m.mu.Unlock()
	return ch
}

func (m *PendingAuthManager) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for state, p := range m.pending {
			if now.After(p.ExpiresAt) {
				p.ResultCh <- AuthResult{Err: fmt.Errorf("auth timed out")}
				delete(m.pending, state)
			}
		}
		m.mu.Unlock()
	}
}

// Complete processes the OAuth callback: exchanges the code for user info,
// then persists the Logto username binding regardless of what SSH username was used.
func (m *PendingAuthManager) Complete(state, code string, cfg *LogtoConfig) error {
	m.mu.Lock()
	pending, ok := m.pending[state]
	if ok {
		delete(m.pending, state)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown or expired state")
	}

	sub, username, err := cfg.exchangeCodeForUserInfo(code)
	if err != nil {
		pending.ResultCh <- AuthResult{Err: err}
		return err
	}

	if err := saveIdentity(cfg.IdentitiesDir, pending.PublicKey, Identity{LogtoSub: sub, Username: username}); err != nil {
		if os.IsExist(err) {
			// Race: key was already bound (e.g. two tabs). Load whatever is stored.
			id, loadErr := loadIdentity(cfg.IdentitiesDir, pending.PublicKey)
			if loadErr != nil {
				pending.ResultCh <- AuthResult{Err: loadErr}
				return loadErr
			}
			if id != nil {
				pending.ResultCh <- AuthResult{Username: id.Username}
				return nil
			}
		}
		pending.ResultCh <- AuthResult{Err: err}
		return err
	}

	pending.ResultCh <- AuthResult{Username: username}
	return nil
}

// newRandomState generates a cryptographically random hex state token.
func newRandomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewCallbackHandler returns an HTTP handler for the OAuth redirect URI.
func NewCallbackHandler(mgr *PendingAuthManager, cfg *LogtoConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" || state == "" {
			http.Error(w, "missing code or state", http.StatusBadRequest)
			return
		}
		if err := mgr.Complete(state, code, cfg); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, callbackHTML("Authentication failed", "Something went wrong. Please try again.", false))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, callbackHTML("Authenticated", "You're in. Head back to your terminal.", true))
	})
}

func callbackHTML(title, message string, ok bool) string {
	color := "#e05"
	symbol := "✗"
	if ok {
		color = "#0c8"
		symbol = "✓"
	}
	return `<!doctype html><html><head><meta charset=utf-8>` +
		`<title>sshland</title>` +
		`<style>*{box-sizing:border-box;margin:0;padding:0}` +
		`body{display:flex;align-items:center;justify-content:center;min-height:100vh;` +
		`background:#0d0d0d;font-family:ui-monospace,monospace;color:#ccc}` +
		`main{text-align:center;padding:2rem}` +
		`.icon{font-size:3rem;color:` + color + `;line-height:1;margin-bottom:1rem}` +
		`h1{font-size:1.1rem;font-weight:600;color:#eee;margin-bottom:.5rem}` +
		`p{font-size:.85rem;color:#666}</style></head>` +
		`<body><main>` +
		`<div class=icon>` + symbol + `</div>` +
		`<h1>` + title + `</h1>` +
		`<p>` + message + `</p>` +
		`</main></body></html>`
}
