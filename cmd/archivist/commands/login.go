package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getarchivist/archivist/cli/pkg/auth"
	"github.com/gookit/goutil/dump"
	"github.com/spf13/cobra"
)

var (
	clientID    = os.Getenv("ARCHIVIST_OAUTH_CLIENT_ID")
	redirectURI = "http://localhost:53682/callback"
	authURL     = os.Getenv("ARCHIVIST_OAUTH_AUTH_URL")
	tokenURL    = os.Getenv("ARCHIVIST_OAUTH_TOKEN_URL")

	defaultClientID = ""
	defaultAuthURL  = ""
	defaultTokenURL = ""
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Archivist API",
	RunE: func(cmd *cobra.Command, args []string) error {

		if clientID == "" {
			clientID = defaultClientID
		}
		if authURL == "" {
			authURL = defaultAuthURL
		}
		if tokenURL == "" {
			tokenURL = defaultTokenURL
		}

		conf := auth.OAuthConfig{
			ClientID:    clientID,
			AuthURL:     authURL,
			TokenURL:    tokenURL,
			RedirectURI: redirectURI,
			Scopes:      []string{"email", "profile"},
		}
		verifier, challenge, err := auth.GeneratePKCE()
		if err != nil {
			return fmt.Errorf("failed to generate PKCE: %w", err)
		}
		authzURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256",
			conf.AuthURL, conf.ClientID, urlEncode(conf.RedirectURI), urlEncode(strings.Join(conf.Scopes, " ")), challenge)
		fmt.Println("Opening browser for login...")
		fmt.Println("If your browser doesn't open, please open the following URL in your browser:")
		fmt.Println(authzURL)
		if err := auth.OpenBrowser(authzURL); err != nil {
			fmt.Printf("Please open the following URL in your browser:\n%s\n", authzURL)
		}
		fmt.Println("Waiting for authentication...")
		code, err := auth.WaitForCode(conf.RedirectURI, 2*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to receive code: %w", err)
		}
		dump.P(code)
		token, err := auth.ExchangeCodeForToken(context.Background(), conf, code, verifier)
		if err != nil {
			return fmt.Errorf("token exchange failed: %w", err)
		}
		dump.P(token)
		if err := auth.StoreToken(token.AccessToken); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}
		fmt.Println("Login successful! Token stored securely.")
		return nil
	},
}

func urlEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func init() {
	RootCmd.AddCommand(loginCmd)
}
