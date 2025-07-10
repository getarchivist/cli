package commands

import (
	"fmt"
	"os"
	"strings"

	"bytes"
	"sync"

	"github.com/manifoldco/promptui"
	"github.com/ohshell/cli/build"
	"github.com/ohshell/cli/pkg/api"
	"github.com/ohshell/cli/pkg/auth"
	"github.com/ohshell/cli/pkg/output"
	"github.com/ohshell/cli/pkg/record"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// CRLFFormatter wraps a logrus.Formatter and replaces \n with \r\n for terminal compatibility
type CRLFFormatter struct {
	logrus.Formatter
}

func (f *CRLFFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	b, err := f.Formatter.Format(entry)
	if err != nil {
		return b, err
	}
	b = bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
	return b, nil
}

var notionFlag bool
var googleFlag bool
var versionFlag bool
var debug bool
var slackAuditFlag bool
var slackChannel string
var noUpload bool

var RootCmd = &cobra.Command{
	Use:   "ohsh",
	Short: "ohshell records your shell session for documentation",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logrus.SetFormatter(&CRLFFormatter{&logrus.TextFormatter{}})
		if debug || os.Getenv("OHSHELL_DEBUG") == "1" || os.Getenv("OHSHELL_DEBUG") == "true" {
			logrus.SetLevel(logrus.DebugLevel)
			logrus.SetOutput(os.Stderr)
		} else {
			logrus.SetLevel(logrus.InfoLevel)
		}
		logrus.Debug("Debug mode enabled")
	},
	Run: func(cmd *cobra.Command, args []string) {
		if versionFlag {
			fmt.Printf("ohsh CLI\n========\nversion: %s\ncommit: %s\nbuild date: %s\n", build.Version, build.Commit, build.Date)
			os.Exit(0)
		}
		token, err := auth.GetToken(auth.RealKeyring{})
		if err != nil {
			fmt.Fprintln(os.Stderr, "[ohsh] You must login first: ohsh login")
			os.Exit(1)
		}

		var wg sync.WaitGroup

		var session *record.Session
		if slackAuditFlag {
			fmt.Fprintf(os.Stderr, "[ohsh] 🎉 Slack audit enabled\n\r")
			session = record.StartSession(record.WithSlackAudit(slackChannel, token))
		} else {
			session = record.StartSession()
		}
		fmt.Printf("[ohsh] Session: %+v\n", session)
		markdown := output.ToMarkdown(session)
		if noUpload {
			fmt.Println("[ohsh] --no-upload flag set, skipping upload.")
			fmt.Printf("[ohsh] Markdown:\n%s\n", markdown)
			if session.SlackThreadTS != "" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					api.SendSlackCompletionAudit(slackChannel, token, session.SlackThreadTS, "")
				}()
			}
			wg.Wait()
			return
		}
		fmt.Printf("[ohsh] Markdown: %s\n", markdown)

		if notionFlag {
			tree, err := api.FetchNotionPageTree(token)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ohsh] Failed to fetch Notion pages: %v\n", err)
				os.Exit(1)
			}

			// Flatten tree for promptui
			var flat []struct {
				ID    string
				Title string
			}
			var walk func(nodes []api.NotionTreeNode, prefix string)
			walk = func(nodes []api.NotionTreeNode, prefix string) {
				for _, n := range nodes {
					flat = append(flat, struct{ ID, Title string }{n.ID, prefix + n.Title})
					if len(n.Children) > 0 {
						walk(n.Children, prefix+"  ")
					}
				}
			}
			walk(tree, "")

			prompt := promptui.Select{
				Label: "Select Notion parent page",
				Items: flat,
				Size:  15,
				Templates: &promptui.SelectTemplates{
					Label:    "{{ . }}",
					Active:   "▶ {{ .Title | cyan }}",
					Inactive: "  {{ .Title }}",
					Selected: "✔ {{ .Title | green }}",
				},
				Searcher: func(input string, index int) bool {
					item := flat[index]
					return containsIgnoreCase(item.Title, input)
				},
			}
			idx, _, err := prompt.Run()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ohsh] Prompt cancelled: %v\n", err)
				os.Exit(1)
			}
			parentID := flat[idx].ID

			// Send doc to Notion with parentID
			resp, err := api.SendMarkdownToNotionWithParent(markdown, token, parentID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ohsh] Failed to upload doc to Notion: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[ohsh] Doc uploaded to Notion! User: %s, Doc ID: %s\n", resp.UserID, resp.ID)
			if session.SlackThreadTS != "" {
				wg.Add(1)
				docURL := fmt.Sprintf("%s/app/runbooks/%s", api.ResolveAPIURL(), resp.ID)
				go func() {
					defer wg.Done()
					api.SendSlackCompletionAudit(slackChannel, token, session.SlackThreadTS, docURL)
				}()
			}
			wg.Wait()
			return
		}
		resp, err := api.SendMarkdownWithDest(markdown, token, notionFlag, googleFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ohsh] Failed to upload doc: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("[ohsh] Doc uploaded! Document URL: %s/app/runbooks/%s\n", api.ResolveAPIURL(), resp.ID)
		if session.SlackThreadTS != "" {
			wg.Add(1)
			docURL := fmt.Sprintf("%s/app/runbooks/%s", api.ResolveAPIURL(), resp.ID)
			go func() {
				defer wg.Done()
				api.SendSlackCompletionAudit(slackChannel, token, session.SlackThreadTS, docURL)
			}()
		}
		wg.Wait()
	},
}

func init() {
	RootCmd.PersistentFlags().BoolVar(&notionFlag, "notion", false, "Push doc to Notion after upload")
	RootCmd.PersistentFlags().BoolVar(&googleFlag, "google", false, "Push doc to Google Docs after upload")
	RootCmd.PersistentFlags().BoolVar(&versionFlag, "version", false, "Print version and exit")
	RootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")
	RootCmd.PersistentFlags().BoolVar(&slackAuditFlag, "slack-audit", false, "Send each command as an audit log to Slack during the session")
	RootCmd.PersistentFlags().StringVar(&slackChannel, "slack-channel", "", "Slack channel to send audit logs to (e.g. #incident-audit)")
	RootCmd.PersistentFlags().BoolVar(&noUpload, "no-upload", false, "Do not upload the generated doc, just print the markdown")
}

// Helper for case-insensitive substring search
func containsIgnoreCase(s, substr string) bool {
	s, substr = strings.ToLower(s), strings.ToLower(substr)
	return strings.Contains(s, substr)
}
