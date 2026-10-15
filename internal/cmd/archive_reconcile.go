package cmd

import (
	"encoding/json"
	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/mail"
)

func newArchiveInspectCommand() *cobra.Command {
	var journal string
	command := &cobra.Command{Use: "archive-inspect", Short: "Inspect retained archive receipts and digests without provider access", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		records, err := mail.InspectArchive(journal, accountFlag)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(records)
	}}
	command.Flags().StringVar(&journal, "journal", "", "Shared private mailbox journal directory")
	return command
}
func newArchiveReconcileCommand() *cobra.Command {
	var journal string
	var r mail.ArchiveResolution
	command := &cobra.Command{Use: "archive-reconcile", Short: "Append an observed outcome for one exact retained receipt; never execute mail", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		r.Account = accountFlag
		if err := mail.ReconcileArchive(journal, r); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	}}
	command.Flags().StringVar(&journal, "journal", "", "Shared private mailbox journal directory")
	command.Flags().StringVar(&r.Message, "message", "", "Reviewed immutable message id")
	command.Flags().StringVar(&r.Source, "source", "", "Receipt source from archive-inspect")
	command.Flags().StringVar(&r.OperationID, "operation", "", "Original operation id")
	command.Flags().StringVar(&r.ReceiptSHA256, "receipt-sha256", "", "Exact receipt digest from archive-inspect")
	command.Flags().StringVar(&r.Outcome, "outcome", "", "Observed archived or notApplied")
	command.Flags().StringVar(&r.Evidence, "evidence", "", "Concrete observation supporting this outcome")
	return command
}
