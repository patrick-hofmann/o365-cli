//go:build darwin || linux

package auth

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func lockCache(path string, action func() error) error {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, filePermission)
	if err != nil {
		return fmt.Errorf("cannot open token cache lock: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return fmt.Errorf("cannot lock token cache: %w", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return action()
}
