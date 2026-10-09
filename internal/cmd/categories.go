package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

func init() { mailCmd.AddCommand(newMailCategoriesCommand()) }

func newMailCategoriesCommand() *cobra.Command {
	command := &cobra.Command{Use: "categories", Short: "Read and update categories on individual messages"}
	command.AddCommand(newReadCategoriesCommand())
	for _, operation := range []string{"add", "remove", "set", "clear"} {
		command.AddCommand(newChangeCategoriesCommand(operation))
	}
	return command
}

func newReadCategoriesCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "get [message-id]", Short: "Show the categories on a message in any folder", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
		RunE: func(cmd *cobra.Command, args []string) error {
			account, err := requireAccount(cmd.Context())
			if err != nil {
				return err
			}
			token, err := getAccessTokenForAccount(cmd.Context(), account)
			if err != nil {
				return err
			}
			client := mail.NewClient(token)
			client.Context = cmd.Context()
			categories, err := client.GetMessageCategories(args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Account string `json:"account"`
					*mail.MessageCategories
				}{account, categories})
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(categories.Categories, "\n"))
			return err
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return command
}

func newChangeCategoriesCommand(operation string) *cobra.Command {
	var asJSON, dryRun bool
	descriptions := map[string]string{
		"add":    "Add categories while preserving existing categories",
		"remove": "Remove only the named categories",
		"set":    "Replace all categories with the supplied names",
		"clear":  "Remove all categories from a message",
	}
	command := &cobra.Command{
		Use: operation + " [message-id] [category...]", Short: descriptions[operation],
		Long:        descriptions[operation] + ".\nNames containing spaces or commas must be quoted. Works across folders.\nUse --dry-run to preview the existing and proposed categories without writing.",
		Annotations: map[string]string{profile.AnnotationKey: "mail.modify"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("a message ID is required")
			}
			return mail.ValidateCategoryChange(operation, args[1:])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			account, err := requireAccount(cmd.Context())
			if err != nil {
				return err
			}
			token, err := getAccessTokenForAccount(cmd.Context(), account)
			if err != nil {
				return err
			}
			client := mail.NewClient(token)
			client.Context = cmd.Context()
			result, err := client.ChangeMessageCategories(args[0], operation, args[1:], dryRun)
			if err != nil {
				return err
			}
			result.Account = account
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Account: %s\nMessage: %s\nBefore: %s\nCategories: %s\nChanged: %t\nApplied: %t\nDry run: %t\n", account, result.MessageID, strings.Join(result.Before, ", "), strings.Join(result.Categories, ", "), result.Changed, result.Applied, result.DryRun)
			return err
		},
	}
	if operation == "clear" {
		command.Use = "clear [message-id]"
	}
	command.Flags().BoolVar(&asJSON, "json", false, "Output the change receipt as JSON")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the change without writing")
	return command
}
