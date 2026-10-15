package mail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ArchiveResolution preserves the original receipt and binds an explicit observation
// to its exact bytes. It never runs a provider operation or restores an old grant.
type ArchiveResolution struct {
	Account       string `json:"account"`
	Message       string `json:"message"`
	Source        string `json:"source"`
	OperationID   string `json:"operationId"`
	ReceiptSHA256 string `json:"receiptSha256"`
	Outcome       string `json:"outcome"`
	Evidence      string `json:"evidence"`
}

func receiptDigest(value any) string {
	body, _ := json.Marshal(value)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}
func resolutionKey(source, operation string) string {
	body, _ := json.Marshal([]string{source, operation})
	return string(body)
}
func (j *archiveJournal) resolutionTarget(r ArchiveResolution) (any, error) {
	if r.Source == "current" {
		value := j.entries[r.OperationID]
		if value == nil || value.Request.Message != r.Message || !strings.EqualFold(value.Request.Account, r.Account) || value.Outcome != "unknown" {
			return nil, errors.New("reconciliation target is not an uncertain current receipt")
		}
		return value, nil
	}
	value, ok := j.history[historyKey(ArchiveHistory{Source: r.Source, OperationID: r.OperationID})]
	if !ok || value.Message != r.Message || !strings.EqualFold(value.Account, r.Account) || (value.State != "unknown" && value.State != "consumed") {
		return nil, errors.New("reconciliation target is not a held historical receipt")
	}
	return value, nil
}
func (j *archiveJournal) validateResolution(r ArchiveResolution) error {
	if (r.Outcome != "archived" && r.Outcome != "notApplied") || len(strings.TrimSpace(r.Evidence)) < 1 || len(r.Evidence) > 2000 {
		return errors.New("reconciliation needs an observed outcome and bounded evidence")
	}
	value, err := j.resolutionTarget(r)
	if err != nil {
		return err
	}
	if receiptDigest(value) != r.ReceiptSHA256 {
		return errors.New("reconciliation receipt changed; inspect its exact digest")
	}
	return nil
}
func ReconcileArchive(root string, r ArchiveResolution) error {
	j, err := openArchiveJournal(root, r.Account)
	if err != nil {
		return err
	}
	defer j.close()
	if err = j.validateResolution(r); err != nil {
		return err
	}
	key := resolutionKey(r.Source, r.OperationID)
	if old, ok := j.resolutions[key]; ok {
		if old != r {
			return errors.New("conflicting immutable archive reconciliation")
		}
		return nil
	}
	j.resolutions[key] = r
	return j.persist()
}

// InspectArchive exposes reviewable receipts and their canonical digest, without authority or provider access.
func InspectArchive(root, account string) ([]map[string]any, error) {
	j, err := openArchiveJournal(root, account)
	if err != nil {
		return nil, err
	}
	defer j.close()
	result := []map[string]any{}
	for key, r := range j.entries {
		result = append(result, map[string]any{"source": "current", "operationId": key, "receipt": r, "sha256": receiptDigest(r), "resolution": j.resolutions[resolutionKey("current", key)]})
	}
	for _, r := range j.history {
		result = append(result, map[string]any{"source": r.Source, "operationId": r.OperationID, "receipt": r, "sha256": receiptDigest(r), "resolution": j.resolutions[resolutionKey(r.Source, r.OperationID)]})
	}
	return result, nil
}
func (j *archiveJournal) resolvedReceipt(receipt *ArchiveReceipt) *ArchiveReceipt {
	if r, ok := j.resolutions[resolutionKey("current", receipt.Request.OperationID)]; ok {
		copy := *receipt
		copy.Outcome = r.Outcome
		copy.Reason = "Explicit reconciliation: " + r.Evidence
		return &copy
	}
	return receipt
}
