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

func newProtectedArchiveCommand() *cobra.Command {
	var request mail.ArchiveRequest
	var journal, gateJSON string
	command := &cobra.Command{Use: "archive", Short: "Archive one exactly reviewed message with a shared durable journal", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.move"}, RunE: func(cmd *cobra.Command, _ []string) error {
		request.Account = accountFlag
		if gateJSON != "" {
			var gate mail.LabelGate
			if err := decodeLabelJSON(gateJSON, &gate); err != nil {
				return err
			}
			request.Gate = &gate
		}
		if err := request.Validate(); err != nil {
			return err
		}
		if journal == "" {
			return errors.New("--journal must name the shared mailbox journal assigned to all callers")
		}
		if err := requireSharedJournal(journal); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getAccessTokenForAccount(ctx, request.Account)
		if err != nil {
			return err
		}
		result, err := mail.ProtectedArchive(ctx, graph.NewClient(token), journal, request)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	command.Flags().StringVar(&request.Message, "message", "", "Reviewed immutable message id")
	command.Flags().StringVar(&request.ContentVersion, "content-version", "", "Reviewed content digest from review-archive")
	command.Flags().StringVar(&request.Sender, "sender", "", "Reviewed sender")
	command.Flags().StringVar(&request.Subject, "subject", "", "Reviewed subject")
	command.Flags().StringVar(&request.Folder, "source-folder", "", "Reviewed Inbox folder id")
	command.Flags().StringVar(&request.Destination, "destination", "", "Exactly archive")
	command.Flags().StringVar(&request.OperationID, "operation", "", "Stable id retained across retries")
	command.Flags().StringVar(&journal, "journal", "", "Existing private shared mailbox journal directory")
	command.Flags().StringVar(&gateJSON, "gate", "", "Exact reviewed label eligibility gate JSON")
	return command
}

func newReviewArchiveCommand() *cobra.Command {
	var message, operation, gateJSON string
	command := &cobra.Command{Use: "review-archive", Short: "Read the complete object to request an exact archive approval", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.read"}, RunE: func(cmd *cobra.Command, _ []string) error {
		request := mail.WorkflowRequest{Operation: "read", Account: accountFlag, Message: message}
		if err := request.Validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getAccessTokenForAccount(ctx, request.Account)
		if err != nil {
			return err
		}
		reply, err := mail.Workflow(ctx, graph.NewClient(token), request)
		if err != nil {
			return err
		}
		if reply.Outcome != "confirmed" || len(reply.Items) != 1 {
			return errors.New("could not read the complete message for review")
		}
		reviewed, err := mail.ReviewArchive(reply.Items[0], request.Account, operation)
		if err != nil {
			return err
		}
		if gateJSON != "" {
			var gate mail.LabelGate
			if err := decodeLabelJSON(gateJSON, &gate); err != nil {
				return err
			}
			reviewed.Gate = &gate
			if err := reviewed.Validate(); err != nil {
				return err
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"protocol": "o365-archive/v1", "request": reviewed, "message": reply.Items[0]})
	}}
	command.Flags().StringVar(&message, "message", "", "Immutable message id")
	command.Flags().StringVar(&operation, "operation", "", "Stable operation id for this review")
	command.Flags().StringVar(&gateJSON, "gate", "", "Label eligibility gate to bind in the exact reviewed request")
	return command
}
