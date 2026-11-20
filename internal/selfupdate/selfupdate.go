// Package selfupdate downloads and applies newer ailock releases from
// GitHub. Checksum verification (SHA256SUMS from the same release)
// protects against corrupted downloads; it is same-origin integrity,
// not a trust boundary against GitHub itself.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are fetched from.
const Repo = "shenghsi/ailock"

// Release describes the latest published release.
type Release struct {
	Tag    string            // e.g. "v0.3.0"
	Assets map[string]string // asset file name → browser_download_url
}

var apiClient = &http.Client{Timeout: 30 * time.Second}

// Latest fetches the newest non-prerelease release from GitHub.
func Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := apiClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("GitHub API unreachable (set https_proxy if GitHub is blocked on this network): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API %s for %s", resp.Status, url)
	}
	var out struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return Release{}, err
	}
	if out.TagName == "" {
		return Release{}, fmt.Errorf("GitHub API returned no tag_name")
	}
	rel := Release{Tag: out.TagName, Assets: map[string]string{}}
	for _, a := range out.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Download fetches a release asset into memory (binaries are a few MB).
func Download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

// AssetName returns the release asset name for a GOOS/GOARCH pair,
// matching build.sh's dist/ naming.
func AssetName(goos, goarch string) string {
	name := fmt.Sprintf("ailock-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// NormalizeTag strips a leading "v" from a release tag.
func NormalizeTag(tag string) string { return strings.TrimPrefix(strings.TrimSpace(tag), "v") }
