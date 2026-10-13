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
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"protocol": "pods-mail/v1", "immutableIds": true, "deltaBoundary": true, "mutationAttempts": 1, "conditionalMoveVerified": false, "conditionalCategoriesVerified": false, "deltaFolders": []string{"inbox", "sentitems"}, "operations": []string{"delta", "read", "move", "categorize", "ensure-categories"}})
	}})
	for _, operation := range []string{"delta", "read", "move", "categorize"} {
		operation := operation // go 1.21: each command keeps its own operation inside its closure
		request := mail.WorkflowRequest{Operation: operation}
		permission := map[string]string{"move": "mail.move", "categorize": "mail.modify"}[operation]
		if permission == "" {
			permission = "mail.read"
		}
		var categories string
		child := &cobra.Command{Use: operation, Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: permission}, RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
			}
			request.Account = accountFlag
			// Every error below happens before a Graph request, so callers receive a not-applied receipt instead of an uncertain failure.
			refuse := func(err error) error { return json.NewEncoder(cmd.OutOrStdout()).Encode(mail.NotApplied(request, err)) }
			if operation == "categorize" && json.Unmarshal([]byte(categories), &request.Categories) != nil {
				return refuse(errors.New("--categories needs a JSON array of names"))
			}
			if err := request.Validate(); err != nil {
				return refuse(err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, request.Account)
			if err != nil {
				return refuse(err)
			}
			reply, err := mail.Workflow(ctx, graph.NewClient(token), request)
			if err != nil {
				return refuse(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
		}}
		if operation == "delta" {
			child.Flags().StringVar(&request.Cursor, "cursor", "", "Exact delta continuation returned by the preceding page")
			child.Flags().StringVar(&request.Folder, "folder", "", "inbox (default) or sentitems")
		}
		if operation == "categorize" {
			child.Flags().StringVar(&request.Message, "message", "", "Immutable message id")
			child.Flags().StringVar(&request.Version, "expected-version", "", "changeKey the categories were read from")
			child.Flags().StringVar(&categories, "categories", "", "Complete JSON array of the message's categories after the change")
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
	command.AddCommand(newEnsureCategoriesCommand())
	return command
}

func newEnsureCategoriesCommand() *cobra.Command {
	var categories string
	child := &cobra.Command{Use: "ensure-categories", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mailbox.categories"}, RunE: func(cmd *cobra.Command, _ []string) error {
		if accountFlag == "" {
			return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
		}
		request := mail.WorkflowRequest{Operation: "ensure-categories", Account: accountFlag}
		refuse := func(err error) error { return json.NewEncoder(cmd.OutOrStdout()).Encode(mail.NotApplied(request, err)) }
		if json.Unmarshal([]byte(categories), &request.Categories) != nil {
			return refuse(errors.New("--categories needs a JSON array of names"))
		}
		if err := request.Validate(); err != nil {
			return refuse(err)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getCategoryListToken(ctx, request.Account)
		if err != nil {
			return refuse(err)
		}
		reply, err := mail.EnsureCategories(ctx, graph.NewClient(token), request)
		if err != nil {
			return refuse(err)
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
	}}
	child.Flags().StringVar(&categories, "categories", "", "JSON array of category names the Outlook category list must contain")
	return child
}
func init() { rootCmd.AddCommand(newWorkflowCommand()) }
