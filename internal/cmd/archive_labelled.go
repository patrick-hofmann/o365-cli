package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/graph"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

func newArchiveLabelledCommand() *cobra.Command {
	var message, label, contentVersion, lock string
	command := &cobra.Command{Use: "archive-labelled", Short: "Archive one Inbox message only while it carries the given label and its assessed content is unchanged", Args: cobra.NoArgs,
		Annotations: map[string]string{profile.AnnotationKey: "mail.move"}, RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
			}
			refuse := func(err error) error {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(&mail.WorkflowReply{Protocol: "pods-mail/v1", Account: accountFlag, Operation: "archive-labelled", Outcome: "notApplied", Reason: err.Error()})
			}
			root, err := lockDirectory(lock)
			if err != nil {
				return refuse(err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute+mail.LockWait)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, accountFlag)
			if err != nil {
				return refuse(err)
			}
			reply, err := mail.ArchiveLabelled(ctx, graph.NewClient(token), root, accountFlag, message, label, contentVersion)
			if err != nil {
				return refuse(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
		}}
	command.Flags().StringVar(&message, "message", "", "Immutable message id")
	command.Flags().StringVar(&label, "label", "", "Category the message must carry to be archived")
	command.Flags().StringVar(&contentVersion, "content-version", "", "assessment-v1 or assessment-v2 digest the decision was made on")
	command.Flags().StringVar(&lock, "lock", "", "Shared lock directory (default: lock_dir from the config)")
	return command
}
