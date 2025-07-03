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

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Clerk using OAuth (PKCE)",
	RunE: func(cmd *cobra.Command, args []string) error {
		clientID := os.Getenv("CLERK_OAUTH_CLIENT_ID")
		redirectURI := "http://localhost:53682/callback"
		authURL := "https://stirred-collie-99.clerk.accounts.dev/oauth/authorize" // Replace with your Clerk domain
		tokenURL := "https://stirred-collie-99.clerk.accounts.dev/oauth/token"    // Replace with your Clerk domain
		if clientID == "" {
			return fmt.Errorf("CLERK_OAUTH_CLIENT_ID not set in environment")
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
