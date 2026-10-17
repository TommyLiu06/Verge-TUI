package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"verge-tui/internal/state"
)

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}
type Installed struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Source  string `json:"source"`
}

func Path(s *state.Store, id string) (string, error) {
	if !state.ValidID(id) {
		return "", errors.New("invalid core version identifier")
	}
	return filepath.Join(s.Root, "cores", id, "mihomo"), nil
}
func List(s *state.Store) ([]Installed, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "cores"))
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, entry := range entries {
		if !entry.IsDir() || !state.ValidID(entry.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Root, "cores", entry.Name(), "manifest.json"))
		if err != nil {
			return nil, err
		}
		var m Installed
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
func Version(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, "-v").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("core version check failed: %w", err)
	}
	v := strings.TrimSpace(string(b))
	if !strings.Contains(strings.ToLower(v), "mihomo") {
		return "", errors.New("binary does not identify as mihomo")
	}
	return strings.Split(v, "\n")[0], nil
}
func Verify(data []byte, digest string) error {
	if !strings.HasPrefix(digest, "sha256:") {
		return errors.New("release asset has no SHA-256 digest")
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != strings.TrimPrefix(digest, "sha256:") {
		return errors.New("core checksum mismatch; installation stopped")
	}
	return nil
}
func Choose(r Release, goos, arch string) (Asset, error) {
	names := []string{"mihomo-" + goos + "-" + arch + "-" + r.Tag + ".gz"}
	if arch == "amd64" {
		names = []string{"mihomo-" + goos + "-amd64-compatible-" + r.Tag + ".gz", "mihomo-" + goos + "-amd64-v1-" + r.Tag + ".gz"}
	}
	for _, name := range names {
		for _, a := range r.Assets {
			if a.Name == name {
				return a, nil
			}
		}
	}
	if r.Prerelease {
		for _, name := range names {
			prefix := strings.TrimSuffix(name, r.Tag+".gz")
			for _, a := range r.Assets {
				if strings.HasPrefix(a.Name, prefix+"alpha-") && strings.HasSuffix(a.Name, ".gz") {
					return a, nil
				}
			}
		}
	}
	return Asset{}, fmt.Errorf("no compatible core asset for %s/%s", goos, arch)
}
func Download(ctx context.Context, s *state.Store, tag string) (Installed, error) {
	if runtime.GOOS != "linux" {
		return Installed{}, errors.New("this first release supports Linux")
	}
	endpoint := releasesAPI + "/latest"
	if tag != "latest" && tag != "" {
		if !state.ValidID(tag) {
			return Installed{}, errors.New("invalid release tag")
		}
		endpoint = releasesAPI + "/tags/" + url.PathEscape(tag)
	}
	client, err := downloadClient(ctx, s)
	if err != nil {
		return Installed{}, err
	}
	defer client.CloseIdleConnections()
	return downloadRelease(ctx, s, client, endpoint)
}

func downloadRelease(ctx context.Context, s *state.Store, client *http.Client, endpoint string) (Installed, error) {
	b, err := fetch(ctx, client, endpoint, 8<<20)
	if err != nil {
		return Installed{}, err
	}
	var release Release
	if err := json.Unmarshal(b, &release); err != nil {
		return Installed{}, err
	}
	if !state.ValidID(release.Tag) {
		return Installed{}, errors.New("invalid release metadata")
	}
	asset, err := Choose(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Installed{}, err
	}
	if !officialAsset(asset) {
		return Installed{}, errors.New("unexpected core download origin")
	}
	if !validDigest(asset.Digest) {
		return Installed{}, errors.New("release asset has no valid SHA-256 digest")
	}
	archive, err := fetch(ctx, client, asset.URL, 100<<20)
	if err != nil {
		return Installed{}, err
	}
	if err := Verify(archive, asset.Digest); err != nil {
		return Installed{}, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return Installed{}, err
	}
	defer gz.Close()
	data, err := io.ReadAll(io.LimitReader(gz, 160<<20+1))
	if err != nil {
		return Installed{}, err
	}
	if len(data) > 160<<20 {
		return Installed{}, errors.New("core binary exceeds size limit")
	}
	return install(ctx, s, releaseID(release, asset), data, asset.URL)
}
func fetch(ctx context.Context, client *http.Client, location string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", location, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "verge-tui")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core request returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("download exceeds size limit")
	}
	return b, nil
}
func Import(ctx context.Context, s *state.Store, id, path string) (Installed, error) {
	if !state.ValidID(id) {
		return Installed{}, errors.New("invalid core version identifier")
	}
	f, err := os.Open(path)
	if err != nil {
		return Installed{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 160<<20+1))
	if err != nil {
		return Installed{}, err
	}
	if len(b) > 160<<20 {
		return Installed{}, errors.New("core binary exceeds size limit")
	}
	return install(ctx, s, id, b, "local")
}
func install(ctx context.Context, s *state.Store, id string, data []byte, source string) (Installed, error) {
	path, err := Path(s, id)
	if err != nil {
		return Installed{}, err
	}
	if _, err := os.Lstat(filepath.Dir(path)); err == nil {
		return Installed{}, errors.New("core version already installed; choose a new identifier")
	} else if !os.IsNotExist(err) {
		return Installed{}, err
	}
	temp, err := os.MkdirTemp(filepath.Join(s.Root, "cores"), ".install-")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "mihomo")
	if err := state.Write(binary, data, 0700); err != nil {
		return Installed{}, err
	}
	v, err := Version(ctx, binary)
	if err != nil {
		return Installed{}, err
	}
	h := sha256.Sum256(data)
	m := Installed{ID: id, Version: v, SHA256: hex.EncodeToString(h[:]), Source: source}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Installed{}, err
	}
	if err := state.Write(filepath.Join(temp, "manifest.json"), b, 0600); err != nil {
		return Installed{}, err
	}
	if err := os.Rename(temp, filepath.Dir(path)); err != nil {
		return Installed{}, err
	}
	return m, nil
}
