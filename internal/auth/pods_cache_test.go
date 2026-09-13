package auth

import (
	"context"
	"fmt"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type syntheticCache string

func (s syntheticCache) Marshal() ([]byte, error) { return []byte(s), nil }
func TestPodsCacheRestartAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c := NewTokenCache(dir)
	if err := c.Export(context.Background(), syntheticCache("synthetic-v1"), cache.ExportHints{}); err != nil {
		t.Fatal(err)
	}
	restarted := NewTokenCache(dir)
	if !restarted.HasToken() || string(restarted.data) != "synthetic-v1" {
		t.Fatal("cache not restored")
	}
	for p, want := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "token.json"): 0600} {
		s, err := os.Stat(p)
		if err != nil || s.Mode().Perm() != want {
			t.Fatalf("permissions %s %v", p, err)
		}
	}
}
func TestPodsSecurityStaleWriterMustNotOverwriteRefresh(t *testing.T) {
	dir := t.TempDir()
	seed := NewTokenCache(dir)
	if err := seed.Export(context.Background(), syntheticCache("old"), cache.ExportHints{}); err != nil {
		t.Fatal(err)
	}
	a, b := NewTokenCache(dir), NewTokenCache(dir)
	if err := a.Export(context.Background(), syntheticCache("refreshed"), cache.ExportHints{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "refreshed" {
		t.Fatal("RELIABILITY FINDING: stale independent cache instance overwrote newer refresh state")
	}
}

func TestPodsCacheRejectsConcurrentExport(t *testing.T) {
	dir := t.TempDir()
	a, b := NewTokenCache(dir), NewTokenCache(dir)
	if err := a.Export(context.Background(), syntheticCache("new"), cache.ExportHints{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Export(context.Background(), syntheticCache("stale"), cache.ExportHints{}); err == nil {
		t.Fatal("stale export succeeded")
	}
	if got := NewTokenCache(dir); string(got.data) != "new" {
		t.Fatal("new cache was overwritten")
	}
}

type invalidCache struct{}

func (invalidCache) Unmarshal([]byte) error {
	return fmt.Errorf("synthetic-secret-token in corrupted input")
}
func TestPodsCacheErrorsDoNotExposeContents(t *testing.T) {
	c := NewTokenCache(t.TempDir())
	if err := c.Export(context.Background(), syntheticCache("synthetic-secret-token"), cache.ExportHints{}); err != nil {
		t.Fatal(err)
	}
	err := c.Replace(context.Background(), invalidCache{}, cache.ReplaceHints{})
	if err == nil || strings.Contains(err.Error(), "synthetic-secret-token") {
		t.Fatalf("unsafe error: %v", err)
	}
}
