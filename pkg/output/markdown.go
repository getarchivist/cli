package output

import (
	"fmt"
	"strings"

	"github.com/getarchivist/archivist/cli/pkg/record"
)

// ToMarkdown generates a simple Markdown representation of the session.
func ToMarkdown(session *record.Session) string {
	var sb strings.Builder
	step := 1
	for _, cmd := range session.Commands {
		trimmed := strings.TrimSpace(strings.ToLower(cmd.Input))
		if trimmed == "exit" {
			continue
		}
		sb.WriteString(fmt.Sprintf("### Step %d\n", step))
		sb.WriteString("**Command:**\n")
		sb.WriteString("```sh\n")
		sb.WriteString(cmd.Input)
		sb.WriteString("\n```")
		if strings.TrimSpace(cmd.Output) != "" {
			sb.WriteString("\n**Output:**\n")
			sb.WriteString("```")
			sb.WriteString(cmd.Output)
			sb.WriteString("\n```")
		}
		sb.WriteString("\n\n")
		step++
	}
	return sb.String()
}
