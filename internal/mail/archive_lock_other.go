//go:build !darwin && !linux

package mail

import (
	"errors"
	"os"
)

func lockArchiveFile(_ string) (*os.File, error) {
	return nil, errors.New("protected mailbox mutation requires a Darwin or Linux shared journal")
}
