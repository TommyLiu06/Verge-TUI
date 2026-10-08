package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Copy the same assets shipped by the package instead of reproducing shell
// function implementations in fixtures.
func ShellScripts(t *testing.T, prefix string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate bundled shell test assets")
	}
	source := filepath.Join(filepath.Dir(file), "..", "..", "packaging", "shell")
	assets := os.DirFS(source)
	if err := fs.WalkDir(assets, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path := filepath.Join(prefix, "shell", name)
		if entry.IsDir() {
			return os.MkdirAll(path, 0755)
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0644)
	}); err != nil {
		t.Fatal(err)
	}
}
