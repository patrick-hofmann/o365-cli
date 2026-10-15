package mail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ArchiveHistory is an immutable migration receipt, never authorization. Old
// content digests cannot become v2 approvals. Consumed and uncertain work holds
// the message across callers and versions until explicit reconciliation.
type ArchiveHistory struct {
	Source        string `json:"source"`
	OperationID   string `json:"operationId"`
	Account       string `json:"account"`
	Message       string `json:"message"`
	State         string `json:"state"`
	Payload       string `json:"payload"`
	PayloadSHA256 string `json:"payloadSha256"`
}

func (r ArchiveHistory) validate(account string) error {
	if r.Source == "" || r.Source == "current" || len(r.Source) > 160 || r.OperationID == "" || len(r.OperationID) > 160 || strings.ContainsAny(r.Source+r.OperationID, "\x00\r\n") || !strings.EqualFold(account, r.Account) {
		return errors.New("invalid archive history identity")
	}
	if err := (WorkflowRequest{Operation: "read", Account: r.Account, Message: r.Message}).Validate(); err != nil {
		return err
	}
	switch r.State {
	case "collecting", "requested", "approved", "denied", "expired", "consumed", "withdrawn", "unknown", "notApplied":
	default:
		return errors.New("invalid historical archive state")
	}
	if len(r.Payload) > 1<<20 || !json.Valid([]byte(r.Payload)) {
		return errors.New("invalid historical archive payload")
	}
	hash := sha256.Sum256([]byte(r.Payload))
	if hex.EncodeToString(hash[:]) != r.PayloadSHA256 {
		return errors.New("historical archive payload digest mismatch")
	}
	return nil
}

// ImportArchiveHistory validates the complete batch before one atomic journal
// update. Re-import is idempotent; conflicting history is never overwritten.
func ImportArchiveHistory(root, account string, records []ArchiveHistory) (map[string]int, error) {
	if len(records) == 0 || len(records) > 10000 {
		return nil, errors.New("archive history must contain 1 to 10000 records")
	}
	journal, err := openArchiveJournal(root, account)
	if err != nil {
		return nil, err
	}
	defer journal.close()
	counts := map[string]int{}
	for _, record := range records {
		if err := record.validate(account); err != nil {
			return nil, err
		}
		key := historyKey(record)
		if old, ok := journal.history[key]; ok {
			before, _ := json.Marshal(old)
			after, _ := json.Marshal(record)
			if string(before) != string(after) {
				return nil, errors.New("conflicting historical archive receipt")
			}
		}
		journal.history[key] = record
		counts[record.State]++
	}
	if err := journal.persist(); err != nil {
		return nil, err
	}
	return counts, nil
}

func historyKey(record ArchiveHistory) string {
	value, _ := json.Marshal([]string{record.Source, record.OperationID})
	return string(value)
}
