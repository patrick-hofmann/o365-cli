package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/graph"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

func decodeLabelJSON(raw string, destination any) error {
	if len(raw) > 32768 {
		return errors.New("label policy JSON exceeds 32 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected trailing label policy JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing label policy JSON")
	}
	return nil
}

func newLabelSnapshotCommand() *cobra.Command {
	var message string
	command := &cobra.Command{Use: "label-snapshot", Short: "Read complete immutable message assessment and current Inbox identity", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.read"}, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := (mail.WorkflowRequest{Operation: "read", Account: accountFlag, Message: message}).Validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getAccessTokenForAccount(ctx, accountFlag)
		if err != nil {
			return err
		}
		snapshot, err := mail.ReadLabelSnapshot(ctx, graph.NewClient(token), accountFlag, message)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(snapshot)
	}}
	command.Flags().StringVar(&message, "message", "", "Immutable message id")
	return command
}

func newDecideLabelCommand() *cobra.Command {
	var requestJSON, journal string
	command := &cobra.Command{Use: "decide-label", Short: "Fresh merge of one owned label dimension under the shared mailbox lock", Args: cobra.NoArgs, Annotations: map[string]string{profile.AnnotationKey: "mail.modify"}, RunE: func(cmd *cobra.Command, _ []string) error {
		var request mail.LabelRequest
		if err := decodeLabelJSON(requestJSON, &request); err != nil {
			return err
		}
		if accountFlag == "" || request.Account != "" && request.Account != accountFlag {
			return errors.New("explicit --account must match the request account")
		}
		request.Account = accountFlag
		if err := request.Validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		token, err := getAccessTokenForAccount(ctx, request.Account)
		if err != nil {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(&mail.LabelReceipt{Protocol: "o365-labels/v1", Request: request, Outcome: "notApplied", Reason: "authentication failed before provider dispatch: " + err.Error()})
		}
		receipt, err := mail.DecideLabel(ctx, graph.NewClient(token), journal, request)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
	}}
	command.Flags().StringVar(&requestJSON, "request", "", "JSON label request: message, contentVersion, operationId, dimension, category and gate")
	command.Flags().StringVar(&journal, "journal", "", "Existing private shared mailbox journal directory, identical for all writers and archivers")
	return command
}

func newLabelInspectCommand() *cobra.Command {
	var journal, operation string
	command := &cobra.Command{Use: "label-inspect", Short: "Inspect retained label write receipts and exact digests", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		receipts, err := mail.InspectLabels(journal, accountFlag)
		if err != nil {
			return err
		}
		if operation != "" {
			filtered := []map[string]any{}
			for _, receipt := range receipts {
				if receipt["operationId"] == operation {
					filtered = append(filtered, receipt)
				}
			}
			receipts = filtered
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(receipts)
	}}
	command.Flags().StringVar(&journal, "journal", "", "Shared private mailbox journal directory")
	command.Flags().StringVar(&operation, "operation", "", "Optional exact operation id")
	return command
}

func newLabelReconcileCommand() *cobra.Command {
	var journal string
	var resolution mail.LabelResolution
	command := &cobra.Command{Use: "label-reconcile", Short: "Append observed outcome for an exact uncertain label receipt; never write mail", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		resolution.Account = accountFlag
		if err := mail.ReconcileLabel(journal, resolution); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resolution)
	}}
	command.Flags().StringVar(&journal, "journal", "", "Shared private mailbox journal directory")
	command.Flags().StringVar(&resolution.Message, "message", "", "Exact immutable message id")
	command.Flags().StringVar(&resolution.OperationID, "operation", "", "Original operation id")
	command.Flags().StringVar(&resolution.ReceiptSHA256, "receipt-sha256", "", "Exact receipt digest from label-inspect")
	command.Flags().StringVar(&resolution.Outcome, "outcome", "", "Observed confirmed or notApplied outcome")
	command.Flags().StringVar(&resolution.Evidence, "evidence", "", "Concrete operator observation; missing labels alone cannot prove non-application")
	return command
}
