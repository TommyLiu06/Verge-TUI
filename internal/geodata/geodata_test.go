package geodata

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"verge-tui/internal/core"
	"verge-tui/internal/profile"
	"verge-tui/internal/state"
)

func testSource(t *testing.T) (string, string) {
	t.Helper()
	bundle := os.Getenv("VERGE_TUI_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("set VERGE_TUI_TEST_BUNDLE to a bundle containing geodata")
	}
	return filepath.Join(bundle, "geodata"), filepath.Join(bundle, "cores", "mihomo")
}

func TestRealGeodataProvisionRepairAndOfflineProfileSwitch(t *testing.T) {
	source, binary := testSource(t)
	s, err := state.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Simulate the partial auto-download that produced the user's warning.
	broken := filepath.Join(s.Root, "runtime", "geoip.metadb")
	if err := os.WriteFile(broken, []byte("partial download"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Install(ctx, source, s.Root, binary); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(broken)
	if err != nil || string(data) == "partial download" {
		t.Fatal("damaged MMDB was not repaired in its original location")
	}
	for _, file := range []string{"geoip.metadb", "GeoIP.dat", "GeoSite.dat"} {
		info, err := os.Stat(filepath.Join(s.Root, "runtime", file))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("missing private database %s: %v", file, err)
		}
	}
	// A valid custom file must retain its name, bytes and modification time.
	info, _ := os.Stat(broken)
	if err := Install(ctx, source, s.Root, binary); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(broken)
	if !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("valid custom MMDB was overwritten")
	}
	if _, err := core.Import(ctx, s, "offline", binary); err != nil {
		t.Fatal(err)
	}
	m := &core.Manager{Store: s}
	if err := m.Use(ctx, "offline"); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	// All geography download URLs have an unsupported scheme, so successful
	// validation/reload proves the databases are available offline.
	for _, mode := range []string{"false", "true"} {
		yaml := "proxies: []\ngeodata-mode: " + mode + "\ngeox-url:\n  mmdb: offline://unavailable\n  geoip: offline://unavailable\n  geosite: offline://unavailable\nrules:\n  - GEOSITE,cn,DIRECT\n  - GEOIP,CN,DIRECT\n  - MATCH,DIRECT\n"
		p, err := profile.Add(s, state.Profile{Name: "Offline geography", Type: "local"}, []byte(yaml))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Apply(ctx, func(c *state.Settings) error { c.Current = p.ID; return nil }); err != nil {
			t.Fatalf("offline switch with geodata-mode=%s: %v", mode, err)
		}
		c, _ := s.Read()
		if c.Current != p.ID {
			t.Fatal("selected profile was not persisted")
		}
	}
}

func TestBundleChecksumAndUnsafeLivePathAreRejected(t *testing.T) {
	source, binary := testSource(t)
	dir := t.TempDir()
	manifest, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Country.mmdb"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("corrupted bundled database accepted")
	}
	s, err := state.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "other-database")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(s.Root, "runtime", "Country.mmdb")); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), source, s.Root, binary); err == nil {
		t.Fatal("symlinked geodata was followed")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "untouched" {
		t.Fatal("unrelated database was modified")
	}
}
