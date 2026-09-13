package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
)

const (
	tokenFileName  = "token.json"
	dirPermission  = 0700
	filePermission = 0600
)

type TokenCache struct {
	cacheDir string
	mu       sync.RWMutex
	data     []byte
	loadErr  error
}

func NewTokenCache(cacheDir string) *TokenCache {
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return &TokenCache{loadErr: errors.New("cannot resolve token cache directory")}
		}
		cacheDir = filepath.Join(home, ".o365-cli")
	}
	t := &TokenCache{cacheDir: cacheDir}
	t.data, t.loadErr = t.read()
	return t
}

func (t *TokenCache) read() ([]byte, error) {
	path := filepath.Join(t.cacheDir, tokenFileName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cannot inspect token cache")
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) || info.Size() > 16<<20 {
		return nil, errors.New("token cache must be a private regular file of at most 16 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read token cache")
	}
	return data, nil
}

func (t *TokenCache) Replace(ctx context.Context, target cache.Unmarshaler, hints cache.ReplaceHints) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := t.read()
	if err != nil {
		return err
	}
	if len(data) > 0 {
		if err := target.Unmarshal(data); err != nil {
			return errors.New("token cache is invalid; reconnect this account")
		}
	}
	t.data, t.loadErr = data, nil
	return nil
}

func (t *TokenCache) Export(ctx context.Context, source cache.Marshaler, hints cache.ExportHints) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.loadErr != nil {
		return t.loadErr
	}
	data, err := source.Marshal()
	if err != nil {
		return errors.New("cannot serialize token cache")
	}
	if len(data) > 16<<20 {
		return errors.New("token cache exceeds 16 MiB")
	}
	return t.withLock(func() error {
		current, err := t.read()
		if err != nil {
			return err
		}
		if !bytes.Equal(current, t.data) {
			return errors.New("token cache changed concurrently; retry authentication")
		}
		if err := t.write(data); err != nil {
			return err
		}
		t.data = bytes.Clone(data)
		return nil
	})
}

func (t *TokenCache) withLock(action func() error) error {
	if err := os.MkdirAll(t.cacheDir, dirPermission); err != nil {
		return fmt.Errorf("cannot create token cache: %w", err)
	}
	info, err := os.Lstat(t.cacheDir)
	if err != nil || !info.IsDir() {
		return errors.New("token cache directory must be private and cannot be a symlink")
	}
	if err := os.Chmod(t.cacheDir, dirPermission); err != nil {
		return errors.New("cannot restrict token cache permissions")
	}
	return lockCache(filepath.Join(t.cacheDir, "token.lock"), action)
}

func (t *TokenCache) write(data []byte) error {
	f, err := os.CreateTemp(t.cacheDir, ".token-*")
	if err != nil {
		return errors.New("cannot create token cache update")
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return errors.New("cannot write token cache update")
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return errors.New("cannot sync token cache update")
	}
	if err := f.Close(); err != nil {
		return errors.New("cannot close token cache update")
	}
	if err := os.Rename(f.Name(), filepath.Join(t.cacheDir, tokenFileName)); err != nil {
		return errors.New("cannot replace token cache")
	}
	return nil
}

// MSAL Export already persisted the update. A later Save must never replay stale bytes.
func (t *TokenCache) Save() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.loadErr
}

func (t *TokenCache) Clear() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.withLock(func() error {
		current, err := t.read()
		if err != nil {
			return err
		}
		if !bytes.Equal(current, t.data) {
			return errors.New("token cache changed concurrently; retry logout")
		}
		if err := os.Remove(filepath.Join(t.cacheDir, tokenFileName)); err != nil && !os.IsNotExist(err) {
			return errors.New("cannot remove token cache")
		}
		t.data, t.loadErr = nil, nil
		return nil
	})
}

func (t *TokenCache) GetCacheDir() string { return t.cacheDir }
func (t *TokenCache) HasToken() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.loadErr == nil && len(t.data) > 0
}
