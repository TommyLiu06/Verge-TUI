package updater

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"verge-tui/internal/core"
)

const repository = "TommyLiu06/Verge-TUI"
const latestAPI = "https://api.github.com/repos/" + repository + "/releases/latest"
const maxArchiveSize = 512 << 20

type Release struct {
	Version   string    `json:"version"`
	Notes     string    `json:"notes"`
	Published time.Time `json:"published"`
	URL       string    `json:"url"`
	Asset     string    `json:"asset"`
	Download  string    `json:"download"`
	Checksum  string    `json:"checksum"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
}

func versionNumbers(value string) ([3]uint64, bool) {
	var out [3]uint64
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return out, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return out, false
			}
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func Newer(latest, current string) bool {
	l, ok := versionNumbers(latest)
	c, valid := versionNumbers(current)
	if !ok || !valid {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func officialURL(value, path string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/"+repository+path
}

func (r Release) validate() error {
	if _, ok := versionNumbers(r.Version); !ok || strings.HasPrefix(r.Version, "v") {
		return errors.New("unsupported release version")
	}
	expected := "verge-tui-" + r.Version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	path := "/releases/download/v" + r.Version + "/" + expected
	if r.Asset != expected || !officialURL(r.Download, path) || !officialURL(r.Checksum, path+".sha256") || !officialURL(r.URL, "/releases/tag/v"+r.Version) {
		return errors.New("release assets do not belong to the official repository or this platform")
	}
	if r.Size < 1 || r.Size > maxArchiveSize || len(r.SHA256) != 64 {
		return errors.New("invalid release size or SHA256 digest")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil {
		return errors.New("invalid release SHA256 digest")
	}
	return nil
}

func fetch(ctx context.Context, client *http.Client, location string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "verge-tui")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("GitHub request refused (HTTP %d); retry later", resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub request failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("GitHub response exceeds the size limit")
	}
	return data, err
}

func readRelease(ctx context.Context, client *http.Client, endpoint string) (Release, error) {
	var out Release
	data, err := fetch(ctx, client, endpoint, 1<<20)
	if err != nil {
		return out, err
	}
	var source struct {
		Tag        string    `json:"tag_name"`
		Body       string    `json:"body"`
		URL        string    `json:"html_url"`
		Published  time.Time `json:"published_at"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return out, err
	}
	if source.Draft || source.Prerelease || !strings.HasPrefix(source.Tag, "v") {
		return out, errors.New("no stable release is available")
	}
	out.Version, out.Notes, out.URL, out.Published = strings.TrimPrefix(source.Tag, "v"), source.Body, source.URL, source.Published
	out.Asset = "verge-tui-" + out.Version + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	for _, asset := range source.Assets {
		switch asset.Name {
		case out.Asset:
			if out.Download != "" || !strings.HasPrefix(asset.Digest, "sha256:") {
				return out, errors.New("release has duplicate assets or is missing a SHA256 digest")
			}
			out.Download, out.SHA256, out.Size = asset.URL, strings.TrimPrefix(asset.Digest, "sha256:"), asset.Size
		case out.Asset + ".sha256":
			if out.Checksum != "" {
				return out, errors.New("release has duplicate checksums")
			}
			out.Checksum = asset.URL
		}
	}
	if out.Download == "" {
		return out, fmt.Errorf("no offline update package is available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return out, out.validate()
}

func (u Updater) client(ctx context.Context) (*http.Client, error) {
	client, err := core.DownloadClient(ctx, u.Store)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
			return errors.New("refusing an insecure or excessive update redirect")
		}
		return nil
	}
	return client, nil
}

func (u Updater) Check(ctx context.Context) (out Status, err error) {
	lock, err := u.lock("operation")
	if err != nil {
		return out, err
	}
	defer lock.Close()
	out, err = u.Status()
	if err != nil {
		return out, err
	}
	out.Phase, out.Error, out.LastAttempt = "checking", "", time.Now()
	if err := u.save(out); err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			out.Phase, out.Error = "error", err.Error()
			out.RetryAfter = time.Now().Add(15 * time.Minute)
		}
		err = errors.Join(err, u.save(out))
	}()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := u.client(ctx)
	if err != nil {
		return out, err
	}
	defer client.CloseIdleConnections()
	release, err := readRelease(ctx, client, latestAPI)
	if err != nil {
		return out, err
	}
	if out.Release == nil || out.Release.SHA256 != release.SHA256 || out.Release.Version != release.Version {
		out.Ready, out.Downloaded, out.IdleSince = false, 0, time.Time{}
	}
	out.Release, out.LastChecked, out.RetryAfter = &release, time.Now(), time.Time{}
	out.Phase = "latest"
	if out.Available() {
		out.Phase = "available"
		if out.Ready {
			out.Phase = "ready"
		}
	}
	return out, nil
}
