package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/graph"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

// lockDirectory prefers the explicit --lock flag over lock_dir from the config file.
func lockDirectory(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if cfg != nil && cfg.LockDir != "" {
		return cfg.LockDir, nil
	}
	return "", errors.New("label writes need the shared lock directory: pass --lock or set lock_dir in the config")
}

// requireSharedJournal refuses a --journal that differs from the configured lock_dir,
// because two directories would mean two locks that no longer exclude each other.
func requireSharedJournal(journal string) error {
	if cfg == nil || cfg.LockDir == "" || filepath.Clean(cfg.LockDir) == filepath.Clean(journal) {
		return nil
	}
	return errors.New("--journal differs from lock_dir; all label writers and archivers must share one lock directory")
}

func newUpdateLabelsCommand() *cobra.Command {
	var update mail.LabelUpdate
	var lock string
	command := &cobra.Command{
		Use: "update-labels", Short: "Remove and add categories in one write under the shared mailbox lock; repeats write nothing twice",
		Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.modify"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account")
			}
			update.Account = accountFlag
			if err := update.Validate(); err != nil {
				return err
			}
			root, err := lockDirectory(lock)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute+mail.LockWait)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, update.Account)
			if err != nil {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(&mail.LabelUpdateReceipt{Protocol: "o365-labels/v2", Request: update, Outcome: "notApplied", Reason: "authentication failed before provider dispatch: " + err.Error()})
			}
			receipt, err := mail.UpdateLabels(ctx, graph.NewClient(token), root, update)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
		},
	}
	command.Flags().StringVar(&update.Message, "message", "", "Immutable message id")
	command.Flags().StringArrayVar(&update.Remove, "remove", nil, "Category to remove when present; repeatable")
	command.Flags().StringArrayVar(&update.Add, "add", nil, "Category to add when missing; repeatable")
	command.Flags().StringVar(&update.ContentVersion, "content-version", "", "Refuse the write unless the assessed content still has this assessment-v1 or assessment-v2 digest")
	command.Flags().StringVar(&lock, "lock", "", "Shared lock directory, identical for all label writers and archivers (default: lock_dir from the config)")
	_ = command.MarkFlagRequired("message")
	return command
}
