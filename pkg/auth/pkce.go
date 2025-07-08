package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	KeyringService  = "archivist-cli"
	KeyringTokenKey = "clerk-access-token"
)

type OAuthConfig struct {
	ClientID    string
	AuthURL     string
	TokenURL    string
	RedirectURI string
	Scopes      []string
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Generate a random PKCE code_verifier and its code_challenge
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// Open the browser to the given URL
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	}
	return fmt.Errorf("unsupported platform")
}

// Start a local HTTP server to receive the OAuth callback
func WaitForCode(redirectURI string, timeout time.Duration) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	codeCh := make(chan string)
	server := &http.Server{Addr: u.Host}

	http.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			codeCh <- ""
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing code", http.StatusBadRequest)
			codeCh <- ""
			return
		}
		fmt.Fprintf(w, "Login successful! You can close this window.")
		codeCh <- code
	})

	go func() {
		_ = server.ListenAndServe()
	}()
	defer server.Close()

	select {
	case code := <-codeCh:
		return code, nil
	case <-time.After(timeout):
		return "", errors.New("timeout waiting for OAuth callback")
	}
}

// Exchange the code for a token
func ExchangeCodeForToken(ctx context.Context, conf OAuthConfig, code, codeVerifier string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", conf.RedirectURI)
	data.Set("client_id", conf.ClientID)
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, "POST", conf.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}
	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	return &token, nil
}

// TokenStore defines the interface for storing and retrieving tokens
// This allows for mocking in tests
type TokenStore interface {
	Set(service, key, value string) error
	Get(service, key string) (string, error)
	Delete(service, key string) error
}

// RealKeyring implements TokenStore using the OS keyring
type RealKeyring struct{}

func (r RealKeyring) Set(service, key, value string) error    { return keyring.Set(service, key, value) }
func (r RealKeyring) Get(service, key string) (string, error) { return keyring.Get(service, key) }
func (r RealKeyring) Delete(service, key string) error        { return keyring.Delete(service, key) }

// StoreToken stores the access token using the provided TokenStore
func StoreToken(store TokenStore, token string) error {
	return store.Set(KeyringService, KeyringTokenKey, token)
}

// GetToken retrieves the access token using the provided TokenStore
func GetToken(store TokenStore) (string, error) {
	return store.Get(KeyringService, KeyringTokenKey)
}
