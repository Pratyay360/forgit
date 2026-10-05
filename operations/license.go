package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var spdxTextBase = "https://raw.githubusercontent.com/spdx/license-list-data/main/text/"

var spdxIndexURL = "https://raw.githubusercontent.com/spdx/license-list-data/main/json/licenses.json"

const spdxIndexMaxAge = 7 * 24 * time.Hour

var httpClient = &http.Client{Timeout: 30 * time.Second}

var licenseYearTokens = []string{"<year>", "[yyyy]", "[year]"}

var licenseHolderTokens = []string{
	"<copyright holders>",
	"<name of copyright owner>",
	"[name of copyright owner]",
	"<name of author>",
	"<copyright holder>",
	"[fullname]",
	"<owner>",
}

var (
	spdxIndexMu sync.Mutex
	spdxIndex   map[string]string
)

func loadSPDXIndex(ctx context.Context) (map[string]string, error) {
	spdxIndexMu.Lock()
	defer spdxIndexMu.Unlock()
	if spdxIndex != nil {
		return spdxIndex, nil
	}

	path, err := spdxIndexCachePath()
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < spdxIndexMaxAge {
		if data, err := os.ReadFile(path); err == nil {
			if index, err := decodeSPDXIndex(data); err == nil && len(index) > 0 {
				spdxIndex = index
				return spdxIndex, nil
			}
		}
	}

	data, err := fetchSPDXIndexBody(ctx)
	if err != nil {
		return nil, err
	}
	index, err := decodeSPDXIndex(data)
	if err != nil {
		return nil, err
	}
	spdxIndex = index
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
	return spdxIndex, nil
}

// fetchSPDXIndexBody downloads the SPDX license list index and returns its
// raw body.
func fetchSPDXIndexBody(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spdxIndexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating index request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching SPDX index: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching SPDX index: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading SPDX index: %w", err)
	}
	return data, nil
}

// decodeSPDXIndex parses the SPDX license list index into a lookup table.
func decodeSPDXIndex(data []byte) (map[string]string, error) {
	var list struct {
		Licenses []struct {
			LicenseID string `json:"licenseId"`
		} `json:"licenses"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("decoding SPDX index: %w", err)
	}
	index := make(map[string]string, len(list.Licenses))
	for _, l := range list.Licenses {
		if l.LicenseID != "" {
			index[strings.ToLower(l.LicenseID)] = l.LicenseID
		}
	}
	return index, nil
}

func spdxIndexCachePath() (string, error) {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "forgit", "spdx-ids.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining cache directory: %w", err)
	}
	return filepath.Join(home, ".cache/forgit/spdx-ids.json"), nil
}

type LicenseMatch struct {
	ID    string
	Score int
}

func SearchLicenses(ctx context.Context, term string) ([]LicenseMatch, error) {
	index, err := loadSPDXIndex(ctx)
	if err != nil {
		return nil, err
	}
	if term == "" {
		out := make([]LicenseMatch, 0, len(index))
		for _, id := range index {
			out = append(out, LicenseMatch{ID: id, Score: 0})
		}
		sortLicenseMatches(out, false)
		return out, nil
	}

	t := strings.ToLower(term)
	var matches []LicenseMatch
	for _, id := range index {
		if s := scoreLicenseID(t, strings.ToLower(id)); s > 0 {
			matches = append(matches, LicenseMatch{ID: id, Score: s})
		}
	}
	sortLicenseMatches(matches, true)
	return matches, nil
}

func BestLicenseMatch(ctx context.Context, term string) (string, error) {
	matches, err := SearchLicenses(ctx, term)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0].ID, nil
}

// scoreLicenseID assigns a score to id against the lowercased term. A score
// of 0 means "no useful match".
func scoreLicenseID(term, id string) int {
	if term == id {
		return 1000
	}
	if id == "" {
		return 0
	}

	if strings.HasPrefix(id, term) {
		return 500
	}

	pos := -1
	score := 0
	prevMatched := -1
	for i := 0; i < len(term); i++ {
		idx := strings.IndexByte(id[pos+1:], term[i])
		if idx < 0 {
			return 0
		}
		pos += idx + 1
		score += 10
		if i > 0 && pos == prevMatched+1 {
			score += 5
		}
		if pos > 0 {
			prev := id[pos-1]
			if prev == '-' || prev == '.' || prev == ' ' {
				score += 3
			}
		}
		prevMatched = pos
	}

	if len(id) > 0 {
		score += 50 - len(id)
		if score < 1 {
			score = 1
		}
	}
	return score
}

func sortLicenseMatches(matches []LicenseMatch, byScore bool) {
	sort.SliceStable(matches, func(i, j int) bool {
		if byScore {
			if matches[i].Score != matches[j].Score {
				return matches[i].Score > matches[j].Score
			}
		}
		return matches[i].ID < matches[j].ID
	})
}

func resolveLicenseID(ctx context.Context, id string) (string, error) {
	index, err := loadSPDXIndex(ctx)
	if err != nil {
		return "", err
	}
	if canonical, ok := index[strings.ToLower(id)]; ok {
		return canonical, nil
	}
	matches, err := SearchLicenses(ctx, id)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("unknown license %q; see https://spdx.org/licenses/", id)
	}
	return matches[0].ID, nil
}

func FetchLicense(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("license id is required")
	}
	resolved, err := resolveLicenseID(ctx, id)
	if err != nil {
		return "", err
	}
	endpoint := spdxTextBase + url.PathEscape(resolved) + ".txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching license %s: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("unknown license %q; see https://spdx.org/licenses/", id)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching license %s: unexpected status %s", id, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading license %s: %w", id, err)
	}
	return string(body), nil
}

func ApplyLicenseTemplate(text, year, holder string) string {
	for _, token := range licenseYearTokens {
		text = strings.ReplaceAll(text, token, year)
	}
	for _, token := range licenseHolderTokens {
		text = strings.ReplaceAll(text, token, holder)
	}
	return text
}
