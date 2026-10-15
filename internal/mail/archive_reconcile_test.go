package mail

import (
	"strings"
	"testing"
)

func TestArchiveReconciliationPreservesHistoryAndRequiresExactObservedReceipt(t *testing.T) {
	for _, outcome := range []string{"archived", "notApplied"} {
		t.Run(outcome, func(t *testing.T) {
			root := archiveDirectory(t)
			record := historyRecord("unknown")
			if _, err := ImportArchiveHistory(root, record.Account, []ArchiveHistory{record}); err != nil {
				t.Fatal(err)
			}
			r := ArchiveResolution{Account: record.Account, Message: record.Message, Source: record.Source, OperationID: record.OperationID, ReceiptSHA256: receiptDigest(record), Outcome: outcome, Evidence: "Owner checked the immutable message in the provider"}
			bad := r
			bad.ReceiptSHA256 = strings.Repeat("0", 64)
			if ReconcileArchive(root, bad) == nil {
				t.Fatal("accepted stale receipt")
			}
			bad = r
			bad.Message = "another"
			if ReconcileArchive(root, bad) == nil {
				t.Fatal("accepted another message")
			}
			bad = r
			bad.Evidence = " "
			if ReconcileArchive(root, bad) == nil {
				t.Fatal("accepted missing observation")
			}
			for i := 0; i < 2; i++ {
				if err := ReconcileArchive(root, r); err != nil {
					t.Fatal(err)
				}
			}
			bad = r
			bad.Outcome = "unknown"
			if ReconcileArchive(root, bad) == nil {
				t.Fatal("accepted conflicting reconciliation")
			}
			j, err := openArchiveJournal(root, r.Account)
			if err != nil {
				t.Fatal(err)
			}
			defer j.close()
			if j.history[historyKey(record)] != record || len(j.resolutions) != 1 {
				t.Fatal("lost original history or duplicated observation")
			}
			previous, err := j.previous(archiveReview(t))
			if outcome == "archived" && err == nil {
				t.Fatal("archived history must continue holding the message")
			}
			if outcome == "notApplied" && (err != nil || previous != nil) {
				t.Fatal("observed non-application should permit a NEW exact decision", err)
			}
		})
	}
}
func TestArchiveReconciliationNeverReplaysOriginalOperation(t *testing.T) {
	root := archiveDirectory(t)
	request := archiveReview(t)
	original := &ArchiveReceipt{Protocol: "o365-archive/v1", Request: request, Outcome: "unknown", Reason: "lost provider response"}
	j, err := openArchiveJournal(root, request.Account)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.save(original); err != nil {
		t.Fatal(err)
	}
	j.close()
	r := ArchiveResolution{Account: request.Account, Message: request.Message, Source: "current", OperationID: request.OperationID, ReceiptSHA256: receiptDigest(original), Outcome: "notApplied", Evidence: "Observed in original folder with unchanged content"}
	if err = ReconcileArchive(root, r); err != nil {
		t.Fatal(err)
	}
	j, err = openArchiveJournal(root, request.Account)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	receipt, err := j.previous(request)
	if err != nil || receipt == nil || receipt.Outcome != "notApplied" {
		t.Fatal("old operation could execute again", err)
	}
	if j.entries[request.OperationID].Outcome != "unknown" {
		t.Fatal("original receipt was overwritten")
	}
	request.OperationID = "explicit-new-operation"
	receipt, err = j.previous(request)
	if receipt != nil || err != nil {
		t.Fatal("new decision cannot proceed", err)
	}
}
