package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadUsesExplicitFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "alt.yaml")
	if err := os.WriteFile(path, []byte("cache_dir: /tmp/alt-cache\n"), 0600); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	File = path
	defer func() { File = ""; viper.Reset() }()

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CacheDir != "/tmp/alt-cache" {
		t.Fatalf("cache_dir = %q, want /tmp/alt-cache", cfg.CacheDir)
	}

	File = filepath.Join(t.TempDir(), "missing.yaml")
	viper.Reset()
	if _, err := Load(); err == nil {
		t.Fatal("missing --config file must be an error")
	}
}
