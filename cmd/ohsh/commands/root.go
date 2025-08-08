package commands

import (
	"fmt"
	"os"
	"strings"

	"bytes"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/creack/termios/raw"
	"github.com/manifoldco/promptui"
	"github.com/ohshell/cli/build"
	"github.com/ohshell/cli/pkg/api"
	"github.com/ohshell/cli/pkg/auth"
	"github.com/ohshell/cli/pkg/output"
	"github.com/ohshell/cli/pkg/record"
	"github.com/ohshell/cli/pkg/spinner"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// UploadPrompt represents the upload confirmation prompt
type UploadPrompt struct {
	choices  []string
	cursor   int
	choice   string
	quitting bool
}

// NewUploadPrompt creates a new upload prompt
func NewUploadPrompt() *UploadPrompt {
	return &UploadPrompt{
		choices: []string{"Yes, upload to Oh Shell!", "No, exit without uploading"},
		cursor:  0,
	}
}

// Init initializes the prompt
func (p *UploadPrompt) Init() tea.Cmd {
	return nil
}

// Update handles key events
func (p *UploadPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.choices)-1 {
				p.cursor++
			}
		case "enter":
			p.choice = p.choices[p.cursor]
			p.quitting = true
			return p, tea.Quit
		case "q", "ctrl+c":
			p.choice = "no"
			p.quitting = true
			return p, tea.Quit
		}
	}
	return p, nil
}

// View renders the prompt
func (p *UploadPrompt) View() string {
	if p.quitting {
		return ""
	}

	var s strings.Builder
	s.WriteString("Upload your session document?\n\n")

	for i, choice := range p.choices {
		cursor := " "
		if p.cursor == i {
			cursor = "▶"
			choice = lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Render(choice)
		}
		s.WriteString(fmt.Sprintf("%s %s\n", cursor, choice))
	}

	s.WriteString("\n(Use ↑/↓ or k/j to navigate, Enter to select, q to quit)\n")

	return s.String()
}

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
var jsonFlag bool

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

		// Show recording feedback
		fmt.Fprintf(os.Stderr, "[ohsh] 📝 Recording session... (commands will be captured)\n\r")
		fmt.Fprintf(os.Stderr, "[ohsh] 💡 Tip: Use Ctrl+C to stop recording and upload your document\n\r")

		// Handle JSON output
		if jsonFlag {
			jsonOutput, err := output.ToJSONString(session)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ohsh] Failed to generate JSON: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(jsonOutput)

			// If slack audit is enabled, send completion message
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

		markdown := output.ToMarkdown(session)

		// Show session summary
		fmt.Printf("[ohsh] 📊 Session captured %d commands\n", len(session.Commands))

		// Check if session is empty
		if len(session.Commands) == 0 {
			fmt.Printf("[ohsh] ⚠️  No commands were captured in this session\n")
			fmt.Printf("[ohsh] 💡 Try running some commands and then exit with Ctrl+C\n")
			return
		}

		fmt.Printf("[ohsh] 🔄 Processing session and preparing document...\n")

		// Restore terminal to cooked mode before running bubbletea
		if term.IsTerminal(int(os.Stdin.Fd())) {
			// Get current terminal state and restore to cooked mode
			oldState, err := raw.TcGetAttr(os.Stdin.Fd())
			if err == nil {
				raw.TcSetAttr(os.Stdin.Fd(), oldState)
			}

			// Additional terminal reset
			fmt.Print("\033[?25h")   // Show cursor
			fmt.Print("\033[?2004l") // Disable bracketed paste
		}

		// Prompt user if they want to upload using bubbletea
		// Drain any pending input to avoid requiring an extra Enter
		drainStdin()
		uploadPrompt := NewUploadPrompt()
		var program *tea.Program
		if tty, err := os.Open("/dev/tty"); err == nil {
			defer tty.Close()
			program = tea.NewProgram(uploadPrompt, tea.WithInput(tty))
		} else {
			program = tea.NewProgram(uploadPrompt)
		}
		result, err := program.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ohsh] Prompt error: %v\n", err)
			os.Exit(1)
		}

		uploadResult := result.(*UploadPrompt)
		if uploadResult.cursor == 1 {
			fmt.Printf("[ohsh] 👋 Exiting without uploading. Your session was recorded but not saved.\n")
			return
		}

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

		if notionFlag {
			s := spinner.New()
			s.Start("Fetching Notion pages...")
			tree, err := api.FetchNotionPageTree(token)
			s.Stop()
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
			uploadSpinner := spinner.New()
			uploadSpinner.Start("Processing session and uploading to Notion...")
			resp, err := api.SendMarkdownToNotionWithParent(markdown, token, parentID)
			uploadSpinner.Stop()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[ohsh] Failed to upload doc to Notion: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("[ohsh] ✅ Document uploaded to Notion successfully!\n")
			fmt.Printf("[ohsh] 📄 Document ID: %s\n", resp.ID)
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
		docSpinner := spinner.New()
		docSpinner.Start("Processing session and generating document...")
		resp, err := api.SendMarkdownWithDest(markdown, token, notionFlag, googleFlag)
		docSpinner.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ohsh] Failed to upload doc: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("[ohsh] ✅ Document uploaded successfully!\n")
		fmt.Printf("[ohsh] 📄 Document URL: %s/app/runbooks/%s\n", api.ResolveAPIURL(), resp.ID)
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
	RootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Output the session as JSON instead of uploading")
}

// Helper for case-insensitive substring search
func containsIgnoreCase(s, substr string) bool {
	s, substr = strings.ToLower(s), strings.ToLower(substr)
	return strings.Contains(s, substr)
}

// drainStdin clears any pending bytes in stdin (non-blocking) to prevent
// stray newlines from being consumed by the next interactive prompt.
func drainStdin() {
	fd := int(os.Stdin.Fd())
	for {
		n, err := unix.IoctlGetInt(fd, unix.TIOCINQ)
		if err != nil || n <= 0 {
			break
		}
		buf := make([]byte, n)
		_, _ = os.Stdin.Read(buf)
	}
}
