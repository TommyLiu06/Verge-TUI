package app

import (
	"context"
	"path/filepath"
	"testing"

	"verge-tui/internal/core"
	"verge-tui/internal/management"
	"verge-tui/internal/state"
	"verge-tui/internal/testutil"
)

func TestSystemClientReadsOfficialCatalogThroughService(t *testing.T) {
	s, err := state.Open(filepath.Join(testutil.Dir(t), "data"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := management.Serve(ctx, s, management.Handlers{ReadData: func(ctx context.Context, kind, id string) (any, error) {
		if kind != "core-releases" || id != "2" {
			t.Error("client did not preserve the official catalog request")
		}
		return PageData{Releases: &core.ReleasePage{Releases: []core.ReleaseOption{{Tag: "v2", ID: "v2"}}, NextPage: 3}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := NewRemote(s, "test")
	defer client.Close(ctx)
	page, err := client.ReadPage(ctx, "core-releases", "2")
	if err != nil || page.Releases == nil || page.Releases.NextPage != 3 || page.Releases.Releases[0].Tag != "v2" {
		t.Fatal("system client could not read catalog metadata", err)
	}
}
