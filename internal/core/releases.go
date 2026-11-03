package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"verge-tui/internal/controller"
	"verge-tui/internal/state"
)

const releasesAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases"
const releasesPerPage = 20

// DownloadClient uses the running managed proxy, including user credentials.
func DownloadClient(ctx context.Context, s *state.Store) (*http.Client, error) {
	return downloadClient(ctx, s)
}

type ReleaseOption struct {
	Tag, ID    string
	Prerelease bool
}
type ReleasePage struct {
	Releases []ReleaseOption
	NextPage int
}

// An explicit managed proxy takes precedence over HTTP_PROXY and NO_PROXY.
// Probe its authenticated controller and listener, rather than trusting stale
// process metadata or accidentally sending credentials to an unrelated port.
func downloadClient(ctx context.Context, s *state.Store) (*http.Client, error) {
	c, err := s.Read()
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	api := controller.New(c)
	defer api.Close()
	var version struct{ Version string }
	if api.Request(probe, http.MethodGet, "/version", nil, &version) == nil && version.Version != "" {
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(c.MixedPort))
		conn, err := (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(probe, "tcp4", address)
		if err == nil {
			conn.Close()
			proxy := &url.URL{Scheme: "http", Host: address}
			if c.InstallMode == "user" {
				proxy.User = url.UserPassword(c.ProxyUser, c.ProxyPassword)
			}
			transport.Proxy = http.ProxyURL(proxy)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &http.Client{Timeout: 3 * time.Minute, Transport: transport}, nil
}

func officialAsset(asset Asset) bool {
	u, err := url.Parse(asset.URL)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && strings.HasPrefix(u.Path, "/MetaCubeX/mihomo/releases/download/")
}

func validDigest(digest string) bool {
	value, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func releaseID(release Release, asset Asset) string {
	if release.Prerelease {
		if i := strings.LastIndex(asset.Name, "-alpha-"); i >= 0 {
			return strings.TrimSuffix(asset.Name[i+1:], ".gz")
		}
	}
	return release.Tag
}

// Fetch only on demand. Pagination keeps metadata memory and request counts
// bounded without hiding older versions behind a hard-coded version list.
func Available(ctx context.Context, s *state.Store, page int) (ReleasePage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := downloadClient(ctx, s)
	if err != nil {
		return ReleasePage{}, err
	}
	defer client.CloseIdleConnections()
	return readReleases(ctx, client, releasesAPI, page)
}

func readReleases(ctx context.Context, client *http.Client, endpoint string, page int) (ReleasePage, error) {
	var out ReleasePage
	if page < 1 || page > 1000 {
		return out, errors.New("invalid release page")
	}
	location := endpoint + "?per_page=" + strconv.Itoa(releasesPerPage) + "&page=" + strconv.Itoa(page)
	data, err := fetch(ctx, client, location, 16<<20)
	if err != nil {
		return out, err
	}
	var releases []Release
	if err := json.Unmarshal(data, &releases); err != nil {
		return out, err
	}
	if len(releases) == releasesPerPage {
		out.NextPage = page + 1
	}
	for _, release := range releases {
		if release.Draft || !state.ValidID(release.Tag) {
			continue
		}
		asset, err := Choose(release, runtime.GOOS, runtime.GOARCH)
		id := releaseID(release, asset)
		if err != nil || !state.ValidID(id) || !officialAsset(asset) || !validDigest(asset.Digest) {
			continue
		}
		out.Releases = append(out.Releases, ReleaseOption{Tag: release.Tag, ID: id, Prerelease: release.Prerelease})
	}
	return out, nil
}
