package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/getarchivist/archivist/cli/pkg/api"
	"github.com/getarchivist/archivist/cli/pkg/auth"
	"github.com/getarchivist/archivist/cli/pkg/output"
	"github.com/getarchivist/archivist/cli/pkg/record"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var notionFlag bool
var googleFlag bool

var RootCmd = &cobra.Command{
	Use:   "archivist",
	Short: "Archivist records your shell session for documentation",
	Run: func(cmd *cobra.Command, args []string) {
		session := record.StartSession()
		markdown := output.ToMarkdown(session)

		token, err := auth.GetToken()
		if err != nil {
			fmt.Fprintln(os.Stderr, "[archivist] You must login first: archivist login")
			os.Exit(1)
		}

		if notionFlag {
			tree, err := api.FetchNotionPageTree(token)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[archivist] Failed to fetch Notion pages: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "[archivist] Prompt cancelled: %v\n", err)
				os.Exit(1)
			}
			parentID := flat[idx].ID

			// Send doc to Notion with parentID
			resp, err := api.SendMarkdownToNotionWithParent(markdown, token, parentID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[archivist] Failed to upload doc to Notion: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[archivist] Doc uploaded to Notion! User: %s, Doc ID: %s\n", resp.UserID, resp.ID)
			return
		}
		resp, err := api.SendMarkdownWithDest(markdown, token, notionFlag, googleFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[archivist] Failed to upload doc: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("[archivist] Doc uploaded! User: %s, Doc ID: %s\n", resp.UserID, resp.ID)
	},
}

func init() {
	RootCmd.PersistentFlags().BoolVar(&notionFlag, "notion", false, "Push doc to Notion after upload")
	RootCmd.PersistentFlags().BoolVar(&googleFlag, "google", false, "Push doc to Google Docs after upload")
}

// Helper for case-insensitive substring search
func containsIgnoreCase(s, substr string) bool {
	s, substr = strings.ToLower(s), strings.ToLower(substr)
	return strings.Contains(s, substr)
}
