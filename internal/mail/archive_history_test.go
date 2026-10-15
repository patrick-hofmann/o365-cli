package mail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func historyRecord(state string) ArchiveHistory {
	payload := "{ \"oldContentVersion\": \"content-v1:old\", \"state\": \"" + state + "\" }"
	hash := sha256.Sum256([]byte(payload))
	return ArchiveHistory{Source: "pods-export", OperationID: "legacy-operation", Account: "pod@example.invalid", Message: "immutable-before", State: state, Payload: payload, PayloadSHA256: hex.EncodeToString(hash[:])}
}
func TestArchiveHistoryPreservesConsumedAndUnknownHoldsAcrossVersions(t *testing.T) {
	for _, state := range []string{"consumed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			root := archiveDirectory(t)
			record := historyRecord(state)
			for i := 0; i < 2; i++ {
				counts, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{record})
				if err != nil || counts[state] != 1 {
					t.Fatalf("import: %v %v", counts, err)
				}
			}
			journal, err := openArchiveJournal(root, record.Account)
			if err != nil {
				t.Fatal(err)
			}
			if len(journal.history) != 1 || journal.history[historyKey(record)].Payload != record.Payload {
				t.Fatal("history was changed or duplicated")
			}
			journal.close()
			request := archiveReview(t)
			request.OperationID = "another-pod"
			request.ContentVersion = "content-v2:" + strings.Repeat("a", 64)
			_, err = ProtectedArchive(context.Background(), nil, root, request)
			if err == nil || !strings.Contains(err.Error(), "historical") {
				t.Fatalf("old state did not hold new request: %v", err)
			}
		})
	}
}
func TestArchiveHistoryIsAtomicAndNeverImportsAuthorization(t *testing.T) {
	root := archiveDirectory(t)
	record := historyRecord("approved")
	invalid := historyRecord("requested")
	invalid.OperationID = "other"
	invalid.PayloadSHA256 = strings.Repeat("0", 64)
	if _, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{record, invalid}); err == nil {
		t.Fatal("accepted altered historical payload")
	}
	journal, err := openArchiveJournal(root, record.Account)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.history) != 0 {
		t.Fatal("partial import persisted")
	}
	journal.close()
	if _, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{record}); err != nil {
		t.Fatal(err)
	}
	journal, err = openArchiveJournal(root, record.Account)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.close()
	previous, err := journal.previous(archiveReview(t))
	if previous != nil || err != nil {
		t.Fatal("historical approval became an execution receipt")
	}
}
func TestArchiveHistoryCannotOverwriteAReplayHold(t *testing.T) {
	root := archiveDirectory(t)
	record := historyRecord("unknown")
	if _, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{record}); err != nil {
		t.Fatal(err)
	}
	changed := historyRecord("notApplied")
	if _, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{changed}); err == nil {
		t.Fatal("overwrote unknown history")
	}
	_, err := ProtectedArchive(context.Background(), nil, root, archiveReview(t))
	if err == nil {
		t.Fatal("hold lost")
	}
}
