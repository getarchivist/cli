package commands

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ohshell/cli/build"
	"github.com/ohshell/cli/pkg/auth"
	"github.com/spf13/cobra"
)

var (
	clientID = os.Getenv("OHSH_OAUTH_CLIENT_ID")
	authURL  = os.Getenv("OHSH_OAUTH_AUTH_URL")
	tokenURL = os.Getenv("OHSH_OAUTH_TOKEN_URL")
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with OhShell API",
	RunE: func(cmd *cobra.Command, args []string) error {

		if clientID == "" {
			clientID = build.DefaultClientID
		}
		if authURL == "" {
			authURL = build.DefaultAuthURL
		}
		if tokenURL == "" {
			tokenURL = build.DefaultTokenURL
		}

		conf := auth.OAuthConfig{
			ClientID:    clientID,
			AuthURL:     authURL,
			TokenURL:    tokenURL,
			RedirectURI: "http://localhost:53682/callback",
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
		token, err := auth.ExchangeCodeForToken(context.Background(), conf, code, verifier)
		if err != nil {
			return fmt.Errorf("token exchange failed: %w", err)
		}
		if err := auth.StoreToken(auth.RealKeyring{}, token.AccessToken); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}
		fmt.Println("Login successful! Token stored securely.")
		fmt.Println("\nWelcome to OhShell! Here's how to get started:")
		fmt.Println("1. Create your first recording:")
		fmt.Println("   $ ohsh")
		fmt.Println("2. Start typing or pasting content")
		fmt.Println("3. Press Ctrl+D when done to save")
		fmt.Println("\nYour docs will appear at https://ohsh.dev/app")
		return nil
	},
}

func urlEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func init() {
	RootCmd.AddCommand(loginCmd)
}
