package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openilink/openilink-hub/internal/auth"
	"github.com/openilink/openilink-hub/internal/config"
	"github.com/openilink/openilink-hub/internal/database"
)

// --- OAuth provider definitions ---

type oauthProvider struct {
	Name         string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	ClientID     string
	ClientSecret string
	Scopes       string
}

func (s *Server) oauthProviders() map[string]*oauthProvider {
	cfg := s.Config
	providers := map[string]*oauthProvider{}
	if cfg.GitHubClientID != "" {
		providers["github"] = &oauthProvider{
			Name:         "github",
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			UserInfoURL:  "https://api.github.com/user",
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			Scopes:       "read:user user:email",
		}
	}
	if cfg.LinuxDoClientID != "" {
		providers["linuxdo"] = &oauthProvider{
			Name:         "linuxdo",
			AuthURL:      "https://connect.linux.do/oauth2/authorize",
			TokenURL:     "https://connect.linux.do/oauth2/token",
			UserInfoURL:  "https://connect.linux.do/api/user",
			ClientID:     cfg.LinuxDoClientID,
			ClientSecret: cfg.LinuxDoClientSecret,
			Scopes:       "",
		}
	}
	return providers
}

// --- OAuth state store (in-memory, short-lived) ---

type oauthStateStore struct {
	mu    sync.Mutex
	store map[string]time.Time
}

func newOAuthStateStore() *oauthStateStore {
	return &oauthStateStore{store: make(map[string]time.Time)}
}

func (s *oauthStateStore) Generate() string {
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)
	s.mu.Lock()
	s.store[state] = time.Now()
	s.mu.Unlock()
	return state
}

func (s *oauthStateStore) Validate(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	created, ok := s.store[state]
	if !ok {
		return false
	}
	delete(s.store, state)
	return time.Since(created) < 10*time.Minute
}

// --- Handlers ---

// GET /api/auth/oauth/providers — list enabled providers
func (s *Server) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	providers := s.oauthProviders()
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"providers": names})
}

// GET /api/auth/oauth/{provider} — redirect to OAuth provider
func (s *Server) handleOAuthRedirect(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	providers := s.oauthProviders()
	p, ok := providers[name]
	if !ok {
		jsonError(w, "unknown provider", http.StatusBadRequest)
		return
	}

	state := s.OAuthStates.Generate()

	params := url.Values{
		"client_id":     {p.ClientID},
		"redirect_uri":  {s.Config.RPOrigin + "/api/auth/oauth/" + name + "/callback"},
		"state":         {state},
		"response_type": {"code"},
	}
	if p.Scopes != "" {
		params.Set("scope", p.Scopes)
	}

	http.Redirect(w, r, p.AuthURL+"?"+params.Encode(), http.StatusFound)
}

// GET /api/auth/oauth/{provider}/callback — handle OAuth callback
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	providers := s.oauthProviders()
	p, ok := providers[name]
	if !ok {
		jsonError(w, "unknown provider", http.StatusBadRequest)
		return
	}

	// Validate state
	state := r.URL.Query().Get("state")
	if !s.OAuthStates.Validate(state) {
		jsonError(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		jsonError(w, "no code provided", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	accessToken, err := exchangeCode(p, s.Config.RPOrigin+"/api/auth/oauth/"+name+"/callback", code)
	if err != nil {
		slog.Error("oauth token exchange failed", "provider", name, "err", err)
		jsonError(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	// Get user info
	providerID, username, email, avatarURL, err := fetchUserInfo(p, accessToken)
	if err != nil {
		slog.Error("oauth user info failed", "provider", name, "err", err)
		jsonError(w, "failed to get user info", http.StatusBadGateway)
		return
	}

	// Find or create user
	user, err := s.findOrCreateOAuthUser(name, providerID, username, email, avatarURL)
	if err != nil {
		slog.Error("oauth user creation failed", "provider", name, "err", err)
		jsonError(w, "login failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if user.Status != database.StatusActive {
		jsonError(w, "account disabled", http.StatusForbidden)
		return
	}

	token, _ := auth.CreateSession(s.DB, user.ID)
	setSessionCookie(w, token)

	// Redirect to frontend
	http.Redirect(w, r, "/", http.StatusFound)
}

// findOrCreateOAuthUser links an OAuth account to an existing user or creates a new one.
func (s *Server) findOrCreateOAuthUser(provider, providerID, username, email, avatarURL string) (*database.User, error) {
	// Check if OAuth account already linked
	oa, err := s.DB.GetOAuthAccount(provider, providerID)
	if err == nil {
		// Already linked — return the user
		return s.DB.GetUserByID(oa.UserID)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// Try to find existing user by email
	var user *database.User
	if email != "" {
		user, err = s.DB.GetUserByEmail(email)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}

	// Create new user if not found
	if user == nil {
		displayName := username
		role := database.RoleMember
		count, _ := s.DB.UserCount()
		if count == 0 {
			role = database.RoleAdmin
		}
		// Generate a unique username to avoid conflicts
		uname := provider + "_" + username
		if _, err := s.DB.GetUserByUsername(uname); err == nil {
			uname = provider + "_" + username + "_" + providerID
		}
		user, err = s.DB.CreateUserFull(uname, email, displayName, "", role)
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
	}

	// Link OAuth account
	if err := s.DB.CreateOAuthAccount(&database.OAuthAccount{
		Provider:   provider,
		ProviderID: providerID,
		UserID:     user.ID,
		Username:   username,
		AvatarURL:  avatarURL,
	}); err != nil {
		return nil, fmt.Errorf("link oauth account: %w", err)
	}

	return user, nil
}

// --- OAuth HTTP helpers ---

func exchangeCode(p *oauthProvider, redirectURI, code string) (string, error) {
	data := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequest("POST", p.TokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// GitHub sometimes returns form-encoded
		vals, _ := url.ParseQuery(string(body))
		result.AccessToken = vals.Get("access_token")
		result.Error = vals.Get("error")
	}
	if result.Error != "" {
		return "", fmt.Errorf("oauth error: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return result.AccessToken, nil
}

func fetchUserInfo(p *oauthProvider, accessToken string) (providerID, username, email, avatarURL string, err error) {
	req, _ := http.NewRequest("GET", p.UserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch p.Name {
	case "github":
		var u struct {
			ID        int    `json:"id"`
			Login     string `json:"login"`
			Email     string `json:"email"`
			AvatarURL string `json:"avatar_url"`
		}
		json.Unmarshal(body, &u)
		return strconv.Itoa(u.ID), u.Login, u.Email, u.AvatarURL, nil

	case "linuxdo":
		var u struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
			Avatar   string `json:"avatar_url"`
			Name     string `json:"name"`
		}
		json.Unmarshal(body, &u)
		name := u.Username
		if name == "" {
			name = u.Name
		}
		return strconv.Itoa(u.ID), name, u.Email, u.Avatar, nil

	default:
		return "", "", "", "", fmt.Errorf("unknown provider: %s", p.Name)
	}
}

// SetupOAuth initializes the OAuth state store. Call from main.
func SetupOAuth(cfg *config.Config) *oauthStateStore {
	return newOAuthStateStore()
}
