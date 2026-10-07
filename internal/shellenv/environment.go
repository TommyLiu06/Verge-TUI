package shellenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mihomo-tui/internal/state"
)

const header = "# mihomo-tui proxy.env v1"
const NoProxy = "localhost,127.0.0.1,::1"

var Variables = []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"}

func FilePath(s *state.Store) string { return filepath.Join(filepath.Dir(s.Root), "proxy.env") }
func ProxyURL(c state.Settings) string {
	u := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(c.MixedPort)}
	if c.InstallMode == "user" {
		u.User = url.UserPassword(c.ProxyUser, c.ProxyPassword)
	}
	return u.String()
}
func Expected(c state.Settings) map[string]string {
	values := map[string]string{}
	for _, name := range Variables {
		values[name] = ProxyURL(c)
	}
	values["no_proxy"], values["NO_PROXY"] = NoProxy, NoProxy
	return values
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func Exports(c state.Settings) string {
	var out strings.Builder
	values := Expected(c)
	for _, name := range Variables {
		fmt.Fprintf(&out, "export %s=%s\n", name, Quote(values[name]))
	}
	return out.String()
}
func Write(s *state.Store) error {
	c, err := s.Read()
	if err != nil {
		return err
	}
	path := FilePath(s)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("proxy.env must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data := fmt.Sprintf("%s\n# install-mode=%s\n# mixed-port=%d\n# api-port=%d\n", header, c.InstallMode, c.MixedPort, c.APIPort) + Exports(c)
	mode := os.FileMode(0600)
	if c.InstallMode == "system" {
		mode = 0644
	}
	return state.Write(path, []byte(data), mode)
}
func Refresh(s *state.Store) error {
	if _, err := os.Lstat(FilePath(s)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := ReadFile(FilePath(s)); err != nil {
		return err
	}
	return Write(s)
}

// ReadFile parses our own fixed-format file without evaluating shell code.
// System installations expose only loopback addresses here, never API tokens.
func ReadFile(path string) (state.Settings, error) {
	var c state.Settings
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, 8193))
	if !scan.Scan() || scan.Text() != header {
		return c, errors.New("unrecognized proxy environment file")
	}
	proxy := ""
	for scan.Scan() {
		line := scan.Text()
		if value, ok := strings.CutPrefix(line, "# install-mode="); ok {
			c.InstallMode = value
		}
		if value, ok := strings.CutPrefix(line, "# mixed-port="); ok {
			c.MixedPort, _ = strconv.Atoi(value)
		}
		if value, ok := strings.CutPrefix(line, "# api-port="); ok {
			c.APIPort, _ = strconv.Atoi(value)
		}
		if value, ok := strings.CutPrefix(line, "export http_proxy='"); ok {
			proxy = strings.TrimSuffix(value, "'")
		}
	}
	if err := scan.Err(); err != nil {
		return c, err
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() != strconv.Itoa(c.MixedPort) || c.MixedPort < 1024 || c.MixedPort > 65535 || c.APIPort < 1024 || c.APIPort > 65535 || c.MixedPort == c.APIPort {
		return c, errors.New("invalid proxy environment metadata")
	}
	if c.InstallMode != "user" && c.InstallMode != "system" {
		return c, errors.New("invalid proxy environment mode")
	}
	if c.InstallMode == "user" {
		if u.User == nil {
			return c, errors.New("private proxy environment is missing credentials")
		}
		c.ProxyUser = u.User.Username()
		c.ProxyPassword, _ = u.User.Password()
	} else if u.User != nil {
		return c, errors.New("shared proxy environment unexpectedly contains credentials")
	}
	return c, nil
}
