package core

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestBaselineCPUAndChecksum(t *testing.T) {
	r := Release{Tag: "v1", Assets: []Asset{{Name: "mihomo-linux-amd64-v3-v1.gz"}, {Name: "mihomo-linux-amd64-compatible-v1.gz"}}}
	a, err := Choose(r, "linux", "amd64")
	if err != nil || a.Name != "mihomo-linux-amd64-compatible-v1.gz" {
		t.Fatalf("baseline CPU asset not selected: %v", err)
	}
	data := []byte("core")
	h := sha256.Sum256(data)
	if err := Verify(data, "sha256:"+hex.EncodeToString(h[:])); err != nil {
		t.Fatal(err)
	}
	if Verify([]byte("modified"), "sha256:"+hex.EncodeToString(h[:])) == nil {
		t.Fatal("accepted modified binary")
	}
	if Verify(data, "") == nil {
		t.Fatal("accepted asset with no digest")
	}
}
