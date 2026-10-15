package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/mail"
)

func newArchiveHistoryCommand() *cobra.Command {
	var source, digest, journal string
	command := &cobra.Command{Use: "archive-import", Short: "Import read-only historical decisions and replay holds into the shared journal", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		file, err := os.Open(source)
		if err != nil {
			return err
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
		if err != nil {
			return err
		}
		if len(body) > 16<<20 {
			return errors.New("migration source exceeds its size bound")
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != digest {
			return errors.New("migration source changed; review its exact digest")
		}
		var input struct {
			Version int                   `json:"version"`
			Records []mail.ArchiveHistory `json:"records"`
		}
		if json.Unmarshal(body, &input) != nil || input.Version != 1 {
			return errors.New("invalid archive migration source")
		}
		counts, err := mail.ImportArchiveHistory(journal, accountFlag, input.Records)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"sourceSha256": digest, "counts": counts, "authorizationImported": false})
	}}
	command.Flags().StringVar(&source, "source", "", "Reviewed history export file")
	command.Flags().StringVar(&digest, "source-sha256", "", "Exact SHA-256 of the reviewed history export")
	command.Flags().StringVar(&journal, "journal", "", "Existing private shared mailbox journal directory")
	return command
}
