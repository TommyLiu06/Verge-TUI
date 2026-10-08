// Package geodata provisions offline geography databases using the bundled core
// to verify formats, without introducing another database parser into the TUI.
package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"verge-tui/internal/state"
)

const maxSize = 64 << 20

var errInvalid = errors.New("invalid geography database")

var Files = []string{"Country.mmdb", "GeoIP.dat", "GeoSite.dat"}

type Asset struct {
	File, Source, Updated, SHA256 string
}
type Manifest struct {
	Schema               int
	Snapshot, Repository string
	Assets               []Asset
}

func Read(source string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Schema != 1 || len(m.Assets) != len(Files) {
		return m, errors.New("invalid bundled geodata manifest")
	}
	seen := map[string]bool{}
	for _, asset := range m.Assets {
		valid := false
		for _, file := range Files {
			valid = valid || asset.File == file
		}
		if !valid || seen[asset.File] {
			return m, errors.New("invalid bundled geodata filename")
		}
		seen[asset.File] = true
		data, err := readRegular(filepath.Join(source, asset.File))
		if err != nil {
			return m, err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != asset.SHA256 {
			return m, fmt.Errorf("bundled %s checksum mismatch", asset.File)
		}
	}
	return m, nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxSize {
		return nil, fmt.Errorf("geodata must be a regular file between 1 byte and 64 MiB: %s", path)
	}
	return os.ReadFile(path)
}

// probe uses non-HTTP URLs: invalid input fails immediately instead of
// downloading replacements. Only the private probe copies can be modified.
func probe(ctx context.Context, binary, dir, file string) error {
	mode, rule := "false", "GEOIP,CN,DIRECT"
	if file == "GeoIP.dat" {
		mode = "true"
	}
	if file == "GeoSite.dat" {
		rule = "GEOSITE,cn,DIRECT"
	}
	payload := fmt.Sprintf("proxies: []\ngeodata-mode: %s\ngeox-url:\n  mmdb: offline://bundled-data-only\n  geoip: offline://bundled-data-only\n  geosite: offline://bundled-data-only\nrules:\n  - %s\n  - MATCH,DIRECT\n", mode, rule)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-t", "-d", dir, "-f", "-")
	cmd.Stdin = strings.NewReader(payload)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLASH_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	// The config is fixed and cannot produce unbounded logs.
	_, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("geodata verification interrupted: %w", ctx.Err())
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() >= 0 {
			return fmt.Errorf("%w: %s", errInvalid, file)
		}
		return fmt.Errorf("geodata verifier failed: %w", err)
	}
	return nil
}

// Verify checks every hash and database format. Its temporary directory is
// created inside source, which is a staging directory owned by the caller.
func Verify(ctx context.Context, source, binary string) error {
	if _, err := Read(source); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(source, ".verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, file := range Files {
		data, err := readRegular(filepath.Join(source, file))
		if err != nil {
			return err
		}
		if err := state.Write(filepath.Join(dir, file), data, 0600); err != nil {
			return err
		}
		if err := probe(ctx, binary, dir, file); err != nil {
			return err
		}
	}
	return nil
}

// Stage returns installation-relative paths that must be replaced. It leaves
// live files untouched: valid custom databases are retained, missing or invalid
// databases are staged for atomic replacement and transactional rollback.
func Stage(ctx context.Context, source, stage, root, binary string) ([]string, error) {
	if _, err := Read(source); err != nil {
		return nil, err
	}
	probeDir, err := os.MkdirTemp(stage, ".geo-probe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(probeDir)
	entries, err := os.ReadDir(filepath.Join(root, "runtime"))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, file := range Files {
		name := file
		found := false
		for _, entry := range entries {
			alias := strings.EqualFold(entry.Name(), file)
			if file == "Country.mmdb" {
				alias = alias || strings.EqualFold(entry.Name(), "geoip.db") || strings.EqualFold(entry.Name(), "geoip.metadb")
			}
			if alias {
				name, found = entry.Name(), true
				break
			}
		}
		if found {
			live := filepath.Join(root, "runtime", name)
			info, err := os.Lstat(live)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("refusing non-regular geodata file: %s", live)
			}
			if info.Size() > maxSize {
				return nil, fmt.Errorf("existing geodata exceeds the 64 MiB verification limit: %s", live)
			}
			// An empty or truncated download is invalid, and can be repaired.
			valid := false
			if info.Size() > 0 && info.Size() <= maxSize {
				data, err := os.ReadFile(live)
				if err != nil {
					return nil, err
				}
				if err := state.Write(filepath.Join(probeDir, file), data, 0600); err != nil {
					return nil, err
				}
				err = probe(ctx, binary, probeDir, file)
				if err != nil && !errors.Is(err, errInvalid) {
					return nil, err
				}
				valid = err == nil
			}
			os.Remove(filepath.Join(probeDir, file))
			if valid {
				continue
			}
		}
		data, err := readRegular(filepath.Join(source, file))
		if err != nil {
			return nil, err
		}
		relative := filepath.Join("data", "runtime", name)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(stage, relative)), 0700); err != nil {
			return nil, err
		}
		if err := state.Write(filepath.Join(stage, relative), data, 0600); err != nil {
			return nil, err
		}
		paths = append(paths, relative)
	}
	manifest, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		return nil, err
	}
	relative := filepath.Join("data", "geodata-bundle.json")
	if err := os.MkdirAll(filepath.Join(stage, "data"), 0700); err != nil {
		return nil, err
	}
	if err := state.Write(filepath.Join(stage, relative), manifest, 0600); err != nil {
		return nil, err
	}
	return append(paths, relative), nil
}

func Install(ctx context.Context, source, root, binary string) error {
	dir, err := os.MkdirTemp(root, ".geodata-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	paths, err := Stage(ctx, source, dir, root, binary)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Stage paths begin with data/, while root is the data directory.
		target := filepath.Join(filepath.Dir(root), path)
		if err := os.Rename(filepath.Join(dir, path), target); err != nil {
			return err
		}
	}
	return nil
}
