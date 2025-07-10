// BackendURL is the base URL for the Archivist backend API.
// It can be set at compile time using -ldflags (e.g., go build -ldflags "-X 'github.com/getarchivist/archivist/cli/pkg/api.BackendURL=https://api.getarchivist.com'")
// In development, if BackendURL is empty or set to "dev", it will use the BACKEND_URL environment variable.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ohshell/cli/build"
	"github.com/sirupsen/logrus"
)

type GenerateDocResponse struct {
	Doc    string `json:"doc"`
	UserID string `json:"user_id"`
	ID     string `json:"id,omitempty"`
}

// NotionTreeNode represents a node in the Notion page/database tree
// Used for TUI selection
type NotionTreeNode struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Icon     *string          `json:"icon,omitempty"`
	Type     string           `json:"type,omitempty"`
	Children []NotionTreeNode `json:"children,omitempty"`
}

// RunbookNotFoundError is returned when a runbook is not found (404)
type RunbookNotFoundError struct {
	ID string
}

func (e *RunbookNotFoundError) Error() string {
	return "runbook not found: " + e.ID
}

func ResolveAPIURL() string {
	if env := os.Getenv("OHSH_API_URL"); env != "" {
		return env
	}
	return build.DefaultAPIURL
}

func SendMarkdown(markdown, token string) (*GenerateDocResponse, error) {
	body := map[string]string{"markdown": markdown}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/generate-doc"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("backend error: %s", resp.Status)
	}
	var out GenerateDocResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func SendMarkdownWithDest(markdown, token string, notion, google bool) (*GenerateDocResponse, error) {
	body := map[string]interface{}{"markdown": markdown}
	if notion {
		body["notion"] = true
	}
	if google {
		body["google"] = true
	}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/generate-doc"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("backend error: %s", resp.Status)
	}
	var out GenerateDocResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchNotionPageTree fetches the Notion page/database tree for the current user
func FetchNotionPageTree(token string) ([]NotionTreeNode, error) {
	url := ResolveAPIURL() + "/api/notion/pages"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("backend error: %s", resp.Status)
	}
	var out struct {
		Tree []NotionTreeNode `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Tree, nil
}

// SendMarkdownToNotionWithParent sends markdown to the backend with a Notion parent page ID
func SendMarkdownToNotionWithParent(markdown, token, parentID string) (*GenerateDocResponse, error) {
	body := map[string]interface{}{
		"markdown":       markdown,
		"notion":         true,
		"notionParentId": parentID,
	}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/generate-doc"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("backend error: %s", resp.Status)
	}
	var out GenerateDocResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchRunbookMarkdown fetches a runbook's markdown by ID from /api/runbooks/{id}
func FetchRunbookMarkdown(id, token string) (string, error) {
	url := ResolveAPIURL() + "/api/runbooks/" + id
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", &RunbookNotFoundError{ID: id}
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("backend error: %s", resp.Status)
	}
	var out struct {
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Markdown, nil
}

// StartSlackAuditThread sends a "runbook started" event to the backend to get a thread_ts
func StartSlackAuditThread(channel, token string) (string, error) {
	if token == "" || channel == "" {
		return "", fmt.Errorf("token and channel must be provided")
	}
	body := map[string]interface{}{
		"type":    "start_runbook",
		"channel": channel,
	}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/slack/audit-log"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("backend error on start_runbook: %s", resp.Status)
	}

	var out struct {
		TS string `json:"ts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode ts from response: %w", err)
	}
	return out.TS, nil
}

// SendSlackCompletionAudit sends a "runbook complete" message to Slack.
func SendSlackCompletionAudit(channel, token, threadTS, docURL string) {
	if token == "" || channel == "" || threadTS == "" {
		return
	}
	body := map[string]interface{}{
		"type":      "complete_runbook",
		"channel":   channel,
		"thread_ts": threadTS,
		"doc_url":   docURL, // can be empty
	}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/slack/audit-log"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		logrus.WithError(err).Error("failed to create slack completion request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logrus.WithError(err).Error("failed to send slack completion request")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logrus.Errorf("failed to send slack completion audit, status: %s, body: %s", resp.Status, string(bodyBytes))
	}
}

// SendSlackAudit sends a command audit log to the backend Slack audit endpoint
func SendSlackAudit(command, channel, token, threadTS string) {
	if token == "" || channel == "" {
		return
	}
	body := map[string]interface{}{
		"type":           "audit_command",
		"command":        command,
		"execution_time": time.Now().Format(time.RFC3339),
		"channel":        channel,
		"thread_ts":      threadTS,
	}
	b, _ := json.Marshal(body)
	url := ResolveAPIURL() + "/api/slack/audit-log"
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}
