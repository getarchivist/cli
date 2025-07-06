// BackendURL is the base URL for the Archivist backend API.
// It can be set at compile time using -ldflags (e.g., go build -ldflags "-X 'github.com/getarchivist/archivist/cli/pkg/api.BackendURL=https://api.getarchivist.com'")
// In development, if BackendURL is empty or set to "dev", it will use the BACKEND_URL environment variable.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/ohshell/cli/build"
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
