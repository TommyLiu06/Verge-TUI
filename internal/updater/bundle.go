package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func hashFile(file string) (string, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("update file must be a regular file")
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), err
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && path.Clean(name) == name && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\r\n\x00")
}

// Extraction starts in a fresh private directory and accepts regular files and
// directories only. No downloaded executable runs before both manifests pass.
func unpack(ctx context.Context, archive, destination string, release Release) (string, error) {
	if err := release.validate(); err != nil {
		return "", err
	}
	info, err := os.Lstat(archive)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != release.Size {
		return "", errors.New("update archive has an unexpected size or type")
	}
	digest, err := hashFile(archive)
	if err != nil {
		return "", err
	}
	if digest != release.SHA256 {
		return "", errors.New("update archive SHA256 verification failed")
	}
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	root := strings.TrimSuffix(release.Asset, ".tar.gz")
	seen := map[string]bool{}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !safeName(name) || name != root && !strings.HasPrefix(name, root+"/") || seen[name] {
			return "", errors.New("update archive contains an unsafe or duplicate path")
		}
		seen[name] = true
		total += header.Size
		if len(seen) > 4096 || header.Size < 0 || header.Size > 256<<20 || total > 1<<30 {
			return "", errors.New("update archive exceeds extraction limits")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if err != nil {
				return "", err
			}
			_, copyErr := io.CopyN(out, reader, header.Size)
			if err := errors.Join(copyErr, out.Close()); err != nil {
				return "", err
			}
		default:
			return "", errors.New("update archive contains a link or unsupported file type")
		}
	}
	// Read to the gzip trailer so a corrupt compressed stream cannot be ignored.
	if n, err := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); err != nil {
		return "", err
	} else if n > 1<<20 {
		return "", errors.New("update archive contains excessive trailing data")
	}
	bundle := filepath.Join(destination, root)
	if err := verifyManifest(ctx, bundle); err != nil {
		return "", err
	}
	for _, name := range []string{"bin/verge-tui", "cores/mihomo"} {
		if err := os.Chmod(filepath.Join(bundle, name), 0700); err != nil {
			return "", err
		}
	}
	return bundle, nil
}

func verifyManifest(ctx context.Context, bundle string) error {
	manifest := filepath.Join(bundle, "SHA256SUMS")
	f, err := os.Open(manifest)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, (1<<20)+1))
	scanner.Buffer(make([]byte, 4096), 8192)
	listed := map[string]bool{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if len(line) < 67 || line[64:66] != "  " {
			return errors.New("invalid bundle checksum manifest")
		}
		name, expected := line[66:], line[:64]
		if !safeName(name) || listed[name] || name == "SHA256SUMS" || len(listed) >= 4096 {
			return errors.New("unsafe or duplicate bundle checksum entry")
		}
		if _, err := hex.DecodeString(expected); err != nil {
			return errors.New("invalid bundle checksum")
		}
		actual, err := hashFile(filepath.Join(bundle, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("bundle checksum verification failed: %s", name)
		}
		listed[name] = true
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, required := range []string{"bin/verge-tui", "cores/mihomo", "install.sh", "uninstall.sh", "CORE_VERSION", "README.md", "LICENSE", "NOTICE.md", "DEPENDENCIES.txt", "geodata/manifest.json", "geodata/Country.mmdb", "geodata/GeoIP.dat", "geodata/GeoSite.dat", "shell/proxy.sh", "shell/fish/init.fish"} {
		if !listed[required] {
			return fmt.Errorf("bundle is missing a required checksum: %s", required)
		}
	}
	return filepath.WalkDir(bundle, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(bundle, file)
		if err != nil {
			return err
		}
		if name != "SHA256SUMS" && !listed[filepath.ToSlash(name)] {
			return fmt.Errorf("bundle contains an unverified file: %s", name)
		}
		return nil
	})
}

type progressWriter struct {
	write func(int64) error
	total int64
}

func (w *progressWriter) Write(data []byte) (int, error) {
	w.total += int64(len(data))
	return len(data), w.write(w.total)
}

func download(ctx context.Context, client *http.Client, release Release, file string, progress func(int64) error) error {
	checksum, err := fetch(ctx, client, release.Checksum, 4096)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(checksum)) != release.SHA256+"  "+release.Asset {
		return errors.New("release checksum file disagrees with the GitHub asset digest")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.Download, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "verge-tui")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > 0 && resp.ContentLength != release.Size {
		return fmt.Errorf("update download failed or size changed (HTTP %d)", resp.StatusCode)
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	p := &progressWriter{write: progress}
	n, copyErr := io.Copy(io.MultiWriter(f, h, p), io.LimitReader(resp.Body, release.Size+1))
	if err := errors.Join(copyErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if n != release.Size || hex.EncodeToString(h.Sum(nil)) != release.SHA256 {
		return errors.New("update download failed size or SHA256 verification")
	}
	return nil
}

func (u Updater) Prepare(ctx context.Context) (out Status, err error) {
	lock, err := u.lock("operation")
	if err != nil {
		return out, err
	}
	defer lock.Close()
	out, err = u.Status()
	if err != nil {
		return out, err
	}
	if !out.Supported {
		return out, errors.New("application updates require an installed offline package")
	}
	if !out.Available() {
		return out, errors.New("check for a newer release before updating")
	}
	if err := out.Release.validate(); err != nil {
		return out, err
	}
	if out.Ready {
		if digest, err := hashFile(filepath.Join(u.directory(), "prepared.tar.gz")); err == nil && digest == out.Release.SHA256 {
			return out, nil
		}
	}
	out.Phase, out.Error, out.Ready, out.Downloaded, out.IdleSince = "downloading", "", false, 0, time.Time{}
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
	stage, err := os.MkdirTemp(u.directory(), ".download-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(stage)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	client, err := u.client(ctx)
	if err != nil {
		return out, err
	}
	client.Timeout = 15 * time.Minute
	defer client.CloseIdleConnections()
	lastProgress := time.Time{}
	archive := filepath.Join(stage, "bundle.tar.gz")
	if err := download(ctx, client, *out.Release, archive, func(n int64) error {
		out.Downloaded = n
		if time.Since(lastProgress) < 250*time.Millisecond {
			return nil
		}
		lastProgress = time.Now()
		return u.save(out)
	}); err != nil {
		return out, err
	}
	out.Phase = "verifying"
	if err := u.save(out); err != nil {
		return out, err
	}
	if _, err := unpack(ctx, archive, stage, *out.Release); err != nil {
		return out, err
	}
	if err := os.Rename(archive, filepath.Join(u.directory(), "prepared.tar.gz")); err != nil {
		return out, err
	}
	out.Phase, out.Ready, out.RetryAfter = "ready", true, time.Time{}
	return out, nil
}
