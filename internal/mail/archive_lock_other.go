//go:build !darwin && !linux

package mail

import (
	"errors"
	"os"
)

func lockArchiveFile(_ string) (*os.File, error) {
	return nil, errors.New("protected archive requires a Darwin or Linux shared journal")
}
