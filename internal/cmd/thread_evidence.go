package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/graph"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

func newThreadEvidenceCommand() *cobra.Command {
	var message, cursor string
	command := &cobra.Command{Use: "thread-evidence", Short: "Read complete same-account conversation pages with explicit Sent folder and draft status", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.read"}, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := (mail.WorkflowRequest{Operation: "read", Account: accountFlag, Message: message}).Validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getAccessTokenForAccount(ctx, accountFlag)
		if err != nil {
			return err
		}
		page, err := mail.ReadThreadEvidence(ctx, graph.NewClient(token), accountFlag, message, cursor)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
	}}
	command.Flags().StringVar(&message, "message", "", "Origin immutable message id")
	command.Flags().StringVar(&cursor, "cursor", "", "Opaque continuation returned by the previous thread page")
	return command
}
