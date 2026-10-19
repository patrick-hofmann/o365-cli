package mail

import (
	"errors"
	"strings"
)

type LabelResolution struct {
	Account       string `json:"account"`
	Message       string `json:"message"`
	OperationID   string `json:"operationId"`
	ReceiptSHA256 string `json:"receiptSha256"`
	Outcome       string `json:"outcome"`
	Evidence      string `json:"evidence"`
}

func (j *archiveJournal) validateLabelResolution(r LabelResolution) error {
	original := j.labels[r.OperationID]
	if original == nil || original.Outcome != "unknown" || original.Request.Message != r.Message || !strings.EqualFold(original.Request.Account, r.Account) || receiptDigest(original) != r.ReceiptSHA256 {
		return errors.New("reconciliation needs the exact uncertain label receipt")
	}
	if (r.Outcome != "confirmed" && r.Outcome != "notApplied") || strings.TrimSpace(r.Evidence) == "" || len(r.Evidence) > 2000 {
		return errors.New("reconciliation needs a confirmed or notApplied outcome and bounded observed evidence")
	}
	return nil
}

func (j *archiveJournal) resolvedLabel(original *LabelReceipt) *LabelReceipt {
	if resolution, ok := j.labelResolutions[original.Request.OperationID]; ok {
		copy := *original
		copy.Outcome = resolution.Outcome
		copy.Reason = "Explicit reconciliation: " + resolution.Evidence
		return &copy
	}
	return original
}

func InspectLabels(root, account string) ([]map[string]any, error) {
	if err := (WorkflowRequest{Operation: "delta", Account: account}).Validate(); err != nil {
		return nil, err
	}
	journal, err := openArchiveJournal(root, account)
	if err != nil {
		return nil, err
	}
	defer journal.close()
	result := []map[string]any{}
	for key, receipt := range journal.labels {
		result = append(result, map[string]any{"operationId": key, "receipt": receipt, "sha256": receiptDigest(receipt), "resolution": journal.labelResolutions[key]})
	}
	return result, nil
}

// ReconcileLabel records an operator observation. Absence of a label alone does
// not establish non-application: Outlook or another writer may have removed it.
func ReconcileLabel(root string, r LabelResolution) error {
	if err := (WorkflowRequest{Operation: "delta", Account: r.Account}).Validate(); err != nil {
		return err
	}
	journal, err := openArchiveJournal(root, r.Account)
	if err != nil {
		return err
	}
	defer journal.close()
	if err := journal.validateLabelResolution(r); err != nil {
		return err
	}
	if old, ok := journal.labelResolutions[r.OperationID]; ok {
		if old != r {
			return errors.New("conflicting immutable label reconciliation")
		}
		return nil
	}
	journal.labelResolutions[r.OperationID] = r
	return journal.persist()
}
