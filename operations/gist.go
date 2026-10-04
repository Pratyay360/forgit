package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Gist is a GitHub Gist.
type Gist struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Public      bool              `json:"public"`
	URL         string            `json:"html_url"`
	Files       map[string]GistFile `json:"files"`
}

// GistFile is a single file within a Gist.
type GistFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// GistInput describes a gist to create.
type GistInput struct {
	Description string
	Public      bool
	Files       map[string]string // filename -> content
}

// Client talks to the GitHub Gist API.
type Client struct {
	token string
}

// New builds a Gist client from a GitHub token.
func New(token string) *Client {
	return &Client{token: token}
}

// Create creates a new gist and returns it.
func (c *Client) Create(ctx context.Context, in GistInput) (Gist, error) {
	payload := gistCreateRequest{
		Description: in.Description,
		Public:      in.Public,
		Files:       make(map[string]gistFileInput, len(in.Files)),
	}
	for name, content := range in.Files {
		payload.Files[name] = gistFileInput{Content: content}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Gist{}, fmt.Errorf("marshaling gist: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/gists", strings.NewReader(string(body)))
	if err != nil {
		return Gist{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Gist{}, fmt.Errorf("creating gist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return Gist{}, fmt.Errorf("creating gist: unexpected status %d", resp.StatusCode)
	}

	var gist Gist
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return Gist{}, fmt.Errorf("decoding gist response: %w", err)
	}
	return gist, nil
}

type gistCreateRequest struct {
	Description string              `json:"description"`
	Public      bool                `json:"public"`
	Files       map[string]gistFileInput `json:"files"`
}

type gistFileInput struct {
	Content string `json:"content"`
}