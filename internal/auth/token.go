package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
)

const (
	tokenFileName  = "token.json"
	dirPermission  = 0700
	filePermission = 0600
)

// TokenCache implements the MSAL cache interface
type TokenCache struct {
	cacheDir string
	mu       sync.RWMutex
	data     []byte
}

// NewTokenCache creates a new token cache
func NewTokenCache(cacheDir string) *TokenCache {
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".o365-cli")
	}

	tc := &TokenCache{
		cacheDir: cacheDir,
	}

	// Try to load existing cache
	tc.load()

	return tc
}

// Replace implements cache.ExportReplace
func (t *TokenCache) Replace(ctx context.Context, cache cache.Unmarshaler, hints cache.ReplaceHints) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.data) == 0 {
		return nil
	}

	return cache.Unmarshal(t.data)
}

// Export implements cache.ExportReplace
func (t *TokenCache) Export(ctx context.Context, cache cache.Marshaler, hints cache.ExportHints) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := cache.Marshal()
	if err != nil {
		return err
	}

	t.data = data
	return t.saveToFile()
}

// load loads the cache from file
func (t *TokenCache) load() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	path := filepath.Join(t.cacheDir, tokenFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cache is OK
		}
		return err
	}

	t.data = data
	return nil
}

// saveToFile saves the cache to file
func (t *TokenCache) saveToFile() error {
	// Create directory if needed
	if err := os.MkdirAll(t.cacheDir, dirPermission); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	path := filepath.Join(t.cacheDir, tokenFileName)

	// Write file with restricted permissions
	if err := os.WriteFile(path, t.data, filePermission); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// Save saves the current cache
func (t *TokenCache) Save() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.saveToFile()
}

// Clear clears the cache
func (t *TokenCache) Clear() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.data = nil

	path := filepath.Join(t.cacheDir, tokenFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token file: %w", err)
	}

	return nil
}

// GetCacheDir returns the cache directory
func (t *TokenCache) GetCacheDir() string {
	return t.cacheDir
}

// HasToken checks if a token is present
func (t *TokenCache) HasToken() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.data) > 0
}
