package commands

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/ohshell/cli/pkg/api"
	"github.com/ohshell/cli/pkg/auth"
)

// --- Lipgloss Styles ---
var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("205"))
	promptStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	codeStyle    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("81")).Padding(0, 1)
	listStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
	headingStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("69"))
	emStyle      = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("244"))
)

// --- Markdown Pretty Rendering ---
func renderMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	var out strings.Builder
	inCode := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "~~~") {
			if !inCode {
				inCode = true
				continue
			} else {
				inCode = false
				continue
			}
		}
		if inCode {
			out.WriteString(codeStyle.Render(line) + "\n")
			continue
		}
		if strings.HasPrefix(trim, "# ") {
			out.WriteString(headingStyle.Render(strings.TrimPrefix(trim, "# ")) + "\n")
			continue
		}
		if strings.HasPrefix(trim, "- ") {
			out.WriteString(listStyle.Render("• "+strings.TrimPrefix(trim, "- ")) + "\n")
			continue
		}
		if strings.HasPrefix(trim, "**") && strings.HasSuffix(trim, "**") {
			out.WriteString(titleStyle.Render(strings.Trim(trim, "*")) + "\n")
			continue
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

// Step represents a single runbook step.
type Step struct {
	Title       string
	Description string
	Command     string
}

// CommandSegment represents a static or placeholder segment in a command.
type CommandSegment struct {
	Text        string // static text
	Placeholder string // placeholder name, if any
	Value       string // user value for placeholder
}

// runCmd is the Cobra command for 'ohsh run <runbook-link>'
var runCmd = &cobra.Command{
	Use:   "run <runbook-link>",
	Short: "Run a runbook step by step in a beautiful TUI",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runbookID := args[0]
		token, err := auth.GetToken()
		if err != nil {
			fmt.Fprintln(os.Stderr, "[ohsh] You must login first: ohsh login")
			os.Exit(1)
		}

		markdown, err := api.FetchRunbookMarkdown(runbookID, token)
		if err != nil {
			if nf, ok := err.(*api.RunbookNotFoundError); ok {
				fmt.Fprintf(os.Stderr, "[ohsh] Runbook not found: %s\n", nf.ID)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "[ohsh] Failed to fetch runbook: %v\n", err)
			os.Exit(1)
		}
		steps := parseRunbookSteps(markdown)
		if len(steps) == 0 {
			fmt.Fprintln(os.Stderr, "[ohsh] No steps found in runbook.")
			os.Exit(1)
		}
		p := tea.NewProgram(NewRunbookModel(steps), tea.WithAltScreen())
		if err := p.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[ohsh] TUI error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(runCmd)
}

// parseRunbookSteps parses Markdown into steps.
func parseRunbookSteps(md string) []Step {
	var steps []Step
	mdParser := goldmark.New()
	d := mdParser.Parser().Parse(text.NewReader([]byte(md)))
	var current Step
	var codeBuilder strings.Builder
	ast.Walk(d, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n.Kind() {
		case ast.KindHeading:
			h := n.(*ast.Heading)
			if h.Level == 3 && entering {
				if current.Title != "" {
					steps = append(steps, current)
					current = Step{}
				}
				current.Title = string(h.Text([]byte(md)))
			}
		case ast.KindParagraph:
			if entering {
				if current.Description == "" {
					current.Description = string(n.Text([]byte(md)))
				}
			}
		case ast.KindFencedCodeBlock:
			if entering {
				codeBuilder.Reset()
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					if i > 0 {
						codeBuilder.WriteByte('\n')
					}
					seg := lines.At(i)
					codeBuilder.Write(seg.Value([]byte(md)))
				}
				current.Command = codeBuilder.String()
			}
		}
		return ast.WalkContinue, nil
	})
	if current.Title != "" {
		steps = append(steps, current)
	}
	return steps
}

// parseCommandWithPlaceholders splits a command into static and placeholder segments.
func parseCommandWithPlaceholders(cmd string) []CommandSegment {
	re := regexp.MustCompile(`<([a-zA-Z0-9_-]+)>`)
	segments := []CommandSegment{}
	last := 0
	for _, loc := range re.FindAllStringSubmatchIndex(cmd, -1) {
		if loc[0] > last {
			segments = append(segments, CommandSegment{Text: cmd[last:loc[0]]})
		}
		segments = append(segments, CommandSegment{
			Placeholder: cmd[loc[2]:loc[3]],
			Value:       "",
		})
		last = loc[1]
	}
	if last < len(cmd) {
		segments = append(segments, CommandSegment{Text: cmd[last:]})
	}
	return segments
}

// --- TUI Model (scaffold) ---

type RunbookModel struct {
	steps    []Step
	index    int
	output   string
	quitting bool
	busy     bool

	// Placeholder editing fields
	segments  []CommandSegment
	activeIdx int  // which placeholder is active
	editing   bool // editing a placeholder
	textInput textinput.Model

	// Full command editing
	fullEditMode  bool
	fullEditInput textinput.Model

	// Output viewport
	outputViewport viewport.Model
	outputFocused  bool
}

func NewRunbookModel(steps []Step) *RunbookModel {
	m := &RunbookModel{steps: steps, index: 0}
	m.initSegments()
	m.textInput = textinput.New()
	m.textInput.Prompt = ""
	m.textInput.CharLimit = 128
	m.textInput.Blur()
	m.fullEditInput = textinput.New()
	m.fullEditInput.Prompt = "> "
	m.fullEditInput.CharLimit = 512
	m.fullEditInput.Blur()
	m.outputViewport = viewport.New(80, 8) // width, height
	m.outputViewport.SetContent("")
	return m
}

// Initialize segments for the current step
func (m *RunbookModel) initSegments() {
	if len(m.steps) == 0 || m.index >= len(m.steps) {
		m.segments = nil
		return
	}
	m.segments = parseCommandWithPlaceholders(m.steps[m.index].Command)
	m.activeIdx = 0
	m.editing = false
}

func (m *RunbookModel) Init() tea.Cmd {
	return nil
}

func (m *RunbookModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Layout: header (step title + description), command (1), footer (2), rest for output
		step := m.steps[m.index]
		descLines := 0
		if step.Description != "" {
			descLines = len(strings.Split(renderMarkdown(step.Description), "\n"))
		}
		headerLines := 5 + descLines // title + description
		commandLines := 1
		footerLines := 2
		avail := msg.Height - headerLines - commandLines - footerLines
		if avail < 3 {
			avail = 3
		}
		m.outputViewport.Width = msg.Width
		m.outputViewport.Height = avail
		return m, nil
	case tea.KeyMsg:
		if m.outputFocused {
			switch msg.String() {
			case "up", "k":
				m.outputViewport.LineUp(1)
				return m, nil
			case "down", "j":
				m.outputViewport.LineDown(1)
				return m, nil
			case "pgup":
				m.outputViewport.ScrollUp(m.outputViewport.Height)
				return m, nil
			case "pgdown":
				m.outputViewport.ScrollDown(m.outputViewport.Height)
				return m, nil
			case "o", "esc":
				m.outputFocused = false
				return m, nil
			}
		}
		if m.fullEditMode {
			switch msg.String() {
			case "enter":
				cmd := m.fullEditInput.Value()
				m.steps[m.index].Command = cmd
				m.fullEditMode = false
				m.fullEditInput.Blur()
				m.initSegments() // re-parse for placeholders
				return m, nil
			case "esc":
				m.fullEditMode = false
				m.fullEditInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.fullEditInput, cmd = m.fullEditInput.Update(msg)
				return m, cmd
			}
		}
		if m.editing {
			switch msg.String() {
			case "enter":
				m.segments[m.activeIdx].Value = m.textInput.Value()
				m.editing = false
				m.textInput.Blur()
				m.steps[m.index].Command = m.FinalCommand()
				return m, nil
			case "esc":
				m.editing = false
				m.textInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		}
		switch msg.String() {
		case "tab":
			if m.editing {
				m.segments[m.activeIdx].Value = m.textInput.Value()
				m.editing = false
				m.textInput.Blur()
			}
			for i := m.activeIdx + 1; i < len(m.segments); i++ {
				if m.segments[i].Placeholder != "" {
					m.activeIdx = i
					m.textInput.SetValue(m.segments[i].Value)
					m.editing = true
					m.textInput.Focus()
					return m, nil
				}
			}
			return m, nil
		case "shift+tab":
			if m.editing {
				m.segments[m.activeIdx].Value = m.textInput.Value()
				m.editing = false
				m.textInput.Blur()
			}
			for i := m.activeIdx - 1; i >= 0; i-- {
				if m.segments[i].Placeholder != "" {
					m.activeIdx = i
					m.textInput.SetValue(m.segments[i].Value)
					m.editing = true
					m.textInput.Focus()
					return m, nil
				}
			}
			return m, nil
		case "e":
			m.fullEditInput.SetValue(m.steps[m.index].Command)
			m.fullEditMode = true
			m.fullEditInput.Focus()
			return m, nil
		case "n":
			if m.index < len(m.steps)-1 {
				m.index++
				m.output = ""
				m.initSegments()
			}
			return m, nil
		case "p":
			if m.index > 0 {
				m.index--
				m.output = ""
				m.initSegments()
			}
			return m, nil
		case "s":
			if m.index < len(m.steps)-1 {
				m.index++
				m.output = ""
				m.initSegments()
			} else {
				m.index = len(m.steps)
			}
			return m, nil
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			cmd := m.FinalCommand()
			m.steps[m.index].Command = cmd
			m.busy = true
			return m, runCommand(cmd)
		case "o":
			if m.output != "" {
				m.outputFocused = !m.outputFocused
			}
			return m, nil
		}
	case commandResultMsg:
		m.output = msg.output
		m.busy = false
		m.outputViewport.SetContent(m.output)
		m.outputViewport.GotoTop()
		return m, nil
	}
	return m, nil
}

// FinalCommand reconstructs the command with placeholder values.
func (m *RunbookModel) FinalCommand() string {
	var b strings.Builder
	for _, seg := range m.segments {
		if seg.Placeholder != "" {
			if seg.Value != "" {
				b.WriteString(seg.Value)
			} else {
				b.WriteString("<" + seg.Placeholder + ">")
			}
		} else {
			b.WriteString(seg.Text)
		}
	}
	return b.String()
}

func (m *RunbookModel) View() string {
	if m.quitting {
		return promptStyle.Render("[ohsh] Exiting runbook.")
	}
	if m.index >= len(m.steps) {
		return promptStyle.Render("[ohsh] Runbook complete!")
	}
	if m.outputFocused {
		step := m.steps[m.index]
		header := titleStyle.Render(fmt.Sprintf("Step %d/%d: %s", m.index+1, len(m.steps), step.Title))
		desc := renderMarkdown(step.Description)
		cmdLine := promptStyle.Render("Command: ")
		for _, seg := range m.segments {
			if seg.Placeholder != "" {
				cmdLine += lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("<" + seg.Placeholder + ">")
			} else {
				cmdLine += seg.Text
			}
		}
		footer := promptStyle.Render("[tab/shift+tab] Edit placeholders  [e] Edit full command  [up/down/pgup/pgdn] Scroll  [o/esc] Exit output view") + "\n" + promptStyle.Render("[enter] Run  [s] Skip  [n] Next  [p] Prev  [q] Quit")
		return header + "\n" + desc + "\n" + cmdLine + "\n" + m.outputViewport.View() + "\n" + footer
	}
	step := m.steps[m.index]
	view := strings.Builder{}
	view.WriteString(titleStyle.Render(fmt.Sprintf("Step %d/%d: %s", m.index+1, len(m.steps), step.Title)) + "\n")
	view.WriteString(renderMarkdown(step.Description) + "\n")
	if m.fullEditMode {
		view.WriteString(promptStyle.Render("[FULL COMMAND EDIT MODE]") + "\n")
		view.WriteString(m.fullEditInput.View() + "\n")
		view.WriteString(promptStyle.Render("[Enter] Save  [Esc] Cancel") + "\n")
		return view.String()
	}
	view.WriteString(promptStyle.Render("Command: "))
	for i, seg := range m.segments {
		if seg.Placeholder != "" {
			if i == m.activeIdx && m.editing {
				view.WriteString("[")
				view.WriteString(m.textInput.View())
				view.WriteString("]")
			} else {
				view.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("<" + seg.Placeholder + ">"))
			}
		} else {
			view.WriteString(seg.Text)
		}
	}
	view.WriteString("\n")
	if m.output != "" {
		if m.outputFocused {
			view.WriteString(promptStyle.Render("[OUTPUT - SCROLL MODE] (up/down/pgup/pgdn/o/esc)") + "\n")
			view.WriteString(m.outputViewport.View() + "\n")
		} else {
			view.WriteString(codeStyle.Render(m.outputViewport.View()) + "\n")
			view.WriteString(promptStyle.Render("[o] Focus output for scrolling") + "\n")
		}
	}
	view.WriteString(promptStyle.Render("[tab/shift+tab] Edit placeholders  [e] Edit full command  [enter] Run  [s] Skip  [n] Next  [p] Prev  [q] Quit") + "\n")
	return view.String()
}

type commandResultMsg struct {
	output string
}

func runCommand(cmd string) tea.Cmd {
	return func() tea.Msg {
		// Use /bin/sh -c for shell features
		c := exec.Command("/bin/sh", "-c", cmd)
		out, err := c.CombinedOutput()
		if err != nil {
			return commandResultMsg{output: fmt.Sprintf("Error: %v\n%s", err, string(out))}
		}
		return commandResultMsg{output: string(out)}
	}
}
