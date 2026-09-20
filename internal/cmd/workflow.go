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

func newWorkflowCommand() *cobra.Command {
	command := &cobra.Command{Use: "workflow", Short: "Explicit account-scoped JSON mail transport for Pods", SilenceUsage: true}
	command.AddCommand(&cobra.Command{Use: "capabilities", Args: cobra.NoArgs, PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil }, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"protocol": "pods-mail/v1", "immutableIds": true, "deltaBoundary": true, "mutationAttempts": 1, "conditionalMoveVerified": false, "operations": []string{"delta", "read", "move"}})
	}})
	for _, operation := range []string{"delta", "read", "move"} {
		request := mail.WorkflowRequest{Operation: operation}
		permission := "mail.read"
		if operation == "move" {
			permission = "mail.move"
		}
		child := &cobra.Command{Use: operation, Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: permission}, RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
			}
			request.Account = accountFlag
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
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
		}}
		if operation == "delta" {
			child.Flags().StringVar(&request.Cursor, "cursor", "", "Exact delta continuation returned by the preceding page")
		}
		if operation == "read" || operation == "move" {
			child.Flags().StringVar(&request.Message, "message", "", "Immutable message id")
		}
		if operation == "move" {
			child.Flags().StringVar(&request.Version, "expected-version", "", "Reviewed changeKey")
			child.Flags().StringVar(&request.Folder, "source-folder", "", "Reviewed exact inbox folder id")
			child.Flags().StringVar(&request.Destination, "destination", "", "Only archive is supported")
		}
		command.AddCommand(child)
	}
	return command
}
func init() { rootCmd.AddCommand(newWorkflowCommand()) }
