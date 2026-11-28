package config

import (
	"os"
	"path/filepath"
	"strings"
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

	AllowMissingFile = true
	defer func() { AllowMissingFile = false }()
	if err := SetValue("lock_dir", "/tmp/locks"); err != nil {
		t.Fatalf("config set must create a missing --config file: %v", err)
	}
	if data, err := os.ReadFile(File); err != nil || !strings.Contains(string(data), "/tmp/locks") {
		t.Fatalf("created file = %q, %v", data, err)
	}
}

func TestSetValueDebug(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("client_id: x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	File = path
	defer func() { File = ""; viper.Reset() }()

	if err := SetValue("debug", "true"); err != nil {
		t.Fatal(err)
	}
	viper.Reset()
	if v, err := GetValue("debug"); err != nil || v != "true" {
		t.Fatalf("debug = %q, %v; want true", v, err)
	}
	if err := SetValue("debug", "yes please"); err == nil {
		t.Fatal("non-boolean debug value must be an error")
	}
}

func TestFilePathFollowsConfigFlag(t *testing.T) {
	defer func() { File = "" }()
	File = ""
	if got, want := FilePath(), filepath.Join(GetConfigDir(), "config.yaml"); got != want {
		t.Fatalf("FilePath() = %q, want %q", got, want)
	}
	File = "/tmp/alt.yaml"
	if got := FilePath(); got != File {
		t.Fatalf("FilePath() = %q, want %q", got, File)
	}
}
