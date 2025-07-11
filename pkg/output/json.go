package output

import (
	"encoding/json"
	"time"

	"github.com/ohshell/cli/pkg/record"
)

// SessionJSON represents a session in JSON format, excluding unexported fields
type SessionJSON struct {
	Commands      []CommandJSON `json:"commands"`
	SlackThreadTS string        `json:"slack_thread_ts,omitempty"`
}

// CommandJSON represents a command in JSON format
type CommandJSON struct {
	Timestamp time.Time `json:"timestamp"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Comment   string    `json:"comment,omitempty"`
	Redacted  bool      `json:"redacted"`
}

// ToJSON generates a JSON representation of the session.
func ToJSON(session *record.Session) ([]byte, error) {
	sessionJSON := SessionJSON{
		Commands:      make([]CommandJSON, 0, len(session.Commands)),
		SlackThreadTS: session.SlackThreadTS,
	}

	for _, cmd := range session.Commands {
		// Skip exit commands as they're filtered out in markdown too
		if cmd.Input == "exit" {
			continue
		}

		sessionJSON.Commands = append(sessionJSON.Commands, CommandJSON{
			Timestamp: cmd.Timestamp,
			Input:     cmd.Input,
			Output:    cmd.Output,
			Comment:   cmd.Comment,
			Redacted:  cmd.Redacted,
		})
	}

	return json.MarshalIndent(sessionJSON, "", "  ")
}

// ToJSONString is a convenience function that returns JSON as a string
func ToJSONString(session *record.Session) (string, error) {
	jsonBytes, err := ToJSON(session)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}
