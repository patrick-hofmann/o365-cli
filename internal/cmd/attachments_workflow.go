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

func newWorkflowAttachmentsCommand() *cobra.Command {
	var message, dir string
	command := &cobra.Command{Use: "attachments", Short: "Save the PDF attachments of one message under their SHA-256", Args: cobra.NoArgs,
		Annotations: map[string]string{profile.AnnotationKey: "mail.read"}, RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
			}
			refuse := func(err error) error {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(&mail.WorkflowReply{Protocol: "pods-mail/v1", Account: accountFlag, Operation: "attachments", Outcome: "notApplied", Reason: err.Error()})
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, accountFlag)
			if err != nil {
				return refuse(err)
			}
			reply, err := mail.SaveMessagePDFs(ctx, graph.NewReadOnlyClient(ctx, token), accountFlag, message, dir)
			if err != nil {
				return refuse(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
		}}
	command.Flags().StringVar(&message, "message", "", "Immutable message id")
	command.Flags().StringVar(&dir, "save-to", "", "Existing absolute directory for <sha256>.pdf files")
	return command
}
