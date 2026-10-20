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

func newAddCategoryCommand() *cobra.Command {
	var message, category string
	command := &cobra.Command{
		Use: "add-category", Short: "Add one category, preserving existing categories; repeated additions are no-ops",
		Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.modify"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account")
			}
			if err := (mail.WorkflowRequest{Operation: "read", Account: accountFlag, Message: message}).Validate(); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, accountFlag)
			if err != nil {
				return err
			}
			result, err := mail.AddCategory(ctx, graph.NewClient(token), accountFlag, message, category)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	command.Flags().StringVar(&message, "message", "", "Immutable message id")
	command.Flags().StringVar(&category, "category", "", "One category name to add")
	_ = command.MarkFlagRequired("message")
	_ = command.MarkFlagRequired("category")
	return command
}
