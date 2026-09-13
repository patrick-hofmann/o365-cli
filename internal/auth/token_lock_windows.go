//go:build windows

package auth

import (
	"golang.org/x/sys/windows"
	"os"
)

func lockCache(path string, action func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, filePermission)
	if err != nil {
		return err
	}
	defer f.Close()
	var overlap windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlap); err != nil {
		return err
	}
	defer windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlap)
	return action()
}
