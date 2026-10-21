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

func newFindCommand() *cobra.Command {
	request := mail.FindRequest{}
	var folder string
	command := &cobra.Command{Use: "find", Short: "Find Inbox messages by exact categories in one server-side query (ids only, newest first)", Args: cobra.NoArgs,
		Annotations: map[string]string{profile.AnnotationKey: "mail.read"}, RunE: func(cmd *cobra.Command, _ []string) error {
			if accountFlag == "" {
				return errors.New("workflow requires an explicit --account; ambient defaults are not accepted")
			}
			request.Account = accountFlag
			refuse := func(err error) error {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(&mail.WorkflowReply{Protocol: "pods-mail/v1", Account: request.Account, Operation: "find", Outcome: "notApplied", Reason: err.Error()})
			}
			if folder != "inbox" {
				return refuse(errors.New("find supports only --folder inbox"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Minute)
			defer cancel()
			token, err := getAccessTokenForAccount(ctx, request.Account)
			if err != nil {
				return refuse(err)
			}
			reply, err := mail.FindMessages(ctx, graph.NewReadOnlyClient(ctx, token), request)
			if err != nil {
				return refuse(err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(reply)
		}}
	command.Flags().StringVar(&folder, "folder", "inbox", "Only inbox is supported")
	command.Flags().StringArrayVar(&request.With, "with", nil, "Exact category the message must carry; repeat for any-of")
	command.Flags().StringArrayVar(&request.Without, "without", nil, "Exact category the message must not carry; repeatable")
	command.Flags().BoolVar(&request.HasAttachments, "has-attachments", false, "Only messages with attachments")
	command.Flags().BoolVar(&request.Oldest, "oldest", false, "Oldest first instead of newest first")
	command.Flags().IntVar(&request.Top, "top", 1, "Number of messages, 1 to 50")
	return command
}
