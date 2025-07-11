package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ohshell/cli/pkg/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToJSON(t *testing.T) {
	// Create a test session
	session := &record.Session{
		Commands: []record.Command{
			{
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Input:     "ls -la",
				Output:    "total 8\ndrwxr-xr-x 2 user user 4096 Jan  1 12:00 .\n",
				Comment:   "List files",
				Redacted:  false,
			},
			{
				Timestamp: time.Date(2023, 1, 1, 12, 1, 0, 0, time.UTC),
				Input:     "echo hello",
				Output:    "hello\n",
				Comment:   "",
				Redacted:  false,
			},
			{
				Timestamp: time.Date(2023, 1, 1, 12, 2, 0, 0, time.UTC),
				Input:     "exit",
				Output:    "",
				Comment:   "",
				Redacted:  false,
			},
		},
		SlackThreadTS: "1234567890.123456",
	}

	// Convert to JSON
	jsonBytes, err := ToJSON(session)
	require.NoError(t, err)

	// Parse back to verify structure
	var sessionJSON SessionJSON
	err = json.Unmarshal(jsonBytes, &sessionJSON)
	require.NoError(t, err)

	// Verify the structure
	assert.Equal(t, "1234567890.123456", sessionJSON.SlackThreadTS)
	assert.Len(t, sessionJSON.Commands, 2) // exit command should be filtered out

	// Verify first command
	assert.Equal(t, "ls -la", sessionJSON.Commands[0].Input)
	assert.Equal(t, "total 8\ndrwxr-xr-x 2 user user 4096 Jan  1 12:00 .\n", sessionJSON.Commands[0].Output)
	assert.Equal(t, "List files", sessionJSON.Commands[0].Comment)
	assert.False(t, sessionJSON.Commands[0].Redacted)
	assert.Equal(t, time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC), sessionJSON.Commands[0].Timestamp)

	// Verify second command
	assert.Equal(t, "echo hello", sessionJSON.Commands[1].Input)
	assert.Equal(t, "hello\n", sessionJSON.Commands[1].Output)
	assert.Empty(t, sessionJSON.Commands[1].Comment)
	assert.False(t, sessionJSON.Commands[1].Redacted)
	assert.Equal(t, time.Date(2023, 1, 1, 12, 1, 0, 0, time.UTC), sessionJSON.Commands[1].Timestamp)
}

func TestToJSONString(t *testing.T) {
	// Create a simple test session
	session := &record.Session{
		Commands: []record.Command{
			{
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Input:     "pwd",
				Output:    "/home/user\n",
				Comment:   "",
				Redacted:  false,
			},
		},
		SlackThreadTS: "",
	}

	// Convert to JSON string
	jsonString, err := ToJSONString(session)
	require.NoError(t, err)

	// Should be valid JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(jsonString), &result)
	require.NoError(t, err)

	// Should contain expected fields
	assert.Contains(t, result, "commands")
	// Note: slack_thread_ts will be omitted when empty due to omitempty tag
}

func TestToJSON_EmptySession(t *testing.T) {
	// Test with empty session
	session := &record.Session{
		Commands:      []record.Command{},
		SlackThreadTS: "",
	}

	jsonBytes, err := ToJSON(session)
	require.NoError(t, err)

	var sessionJSON SessionJSON
	err = json.Unmarshal(jsonBytes, &sessionJSON)
	require.NoError(t, err)

	assert.Empty(t, sessionJSON.Commands)
	assert.Empty(t, sessionJSON.SlackThreadTS)
}

func TestToJSON_WithRedactedCommand(t *testing.T) {
	// Test with redacted command
	session := &record.Session{
		Commands: []record.Command{
			{
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Input:     "sudo password123",
				Output:    "Command executed successfully",
				Comment:   "Sensitive command",
				Redacted:  true,
			},
		},
		SlackThreadTS: "",
	}

	jsonBytes, err := ToJSON(session)
	require.NoError(t, err)

	var sessionJSON SessionJSON
	err = json.Unmarshal(jsonBytes, &sessionJSON)
	require.NoError(t, err)

	assert.Len(t, sessionJSON.Commands, 1)
	assert.True(t, sessionJSON.Commands[0].Redacted)
	assert.Equal(t, "sudo password123", sessionJSON.Commands[0].Input)
	assert.Equal(t, "Sensitive command", sessionJSON.Commands[0].Comment)
}

func TestToJSON_FilterExitCommands(t *testing.T) {
	// Test that exit commands are filtered out
	session := &record.Session{
		Commands: []record.Command{
			{
				Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Input:     "ls",
				Output:    "file1.txt\n",
				Comment:   "",
				Redacted:  false,
			},
			{
				Timestamp: time.Date(2023, 1, 1, 12, 1, 0, 0, time.UTC),
				Input:     "exit",
				Output:    "",
				Comment:   "",
				Redacted:  false,
			},
			{
				Timestamp: time.Date(2023, 1, 1, 12, 2, 0, 0, time.UTC),
				Input:     "pwd",
				Output:    "/home/user\n",
				Comment:   "",
				Redacted:  false,
			},
		},
		SlackThreadTS: "",
	}

	jsonBytes, err := ToJSON(session)
	require.NoError(t, err)

	var sessionJSON SessionJSON
	err = json.Unmarshal(jsonBytes, &sessionJSON)
	require.NoError(t, err)

	// Should only have 2 commands (exit filtered out)
	assert.Len(t, sessionJSON.Commands, 2)
	assert.Equal(t, "ls", sessionJSON.Commands[0].Input)
	assert.Equal(t, "pwd", sessionJSON.Commands[1].Input)
}
