package mail

import (
	"context"
	"testing"
)

func TestAddCategoryPreservesExistingAndSkipsRepeat(t *testing.T) {
	mailbox := newLabelMailbox(t)
	client := mailbox.client(t)
	for i := 0; i < 2; i++ {
		result, err := AddCategory(context.Background(), client, "pod@example.invalid", "immutable-before", "Selected")
		if err != nil || result.Outcome != "confirmed" || result.Operation != "add-category" {
			t.Fatalf("unexpected result: %+v %v", result, err)
		}
	}
	if mailbox.patches != 1 {
		t.Fatalf("patches=%d", mailbox.patches)
	}
	labels := mailbox.raw["categories"].([]string)
	if len(labels) != 2 || labels[0] != "Unrelated" || labels[1] != "Selected" {
		t.Fatalf("labels=%v", labels)
	}
}

func TestAddCategoryLostResponseDoesNotDuplicateAppliedWrite(t *testing.T) {
	mailbox := newLabelMailbox(t)
	mailbox.mode = "timeout-applied"
	client := mailbox.client(t)
	first, err := AddCategory(context.Background(), client, "pod@example.invalid", "immutable-before", "Selected")
	if err != nil || first.Outcome != "unknown" {
		t.Fatalf("first: %+v %v", first, err)
	}
	next, err := AddCategory(context.Background(), client, "pod@example.invalid", "immutable-before", "Selected")
	if err != nil || next.Outcome != "confirmed" || mailbox.patches != 1 {
		t.Fatalf("repeat: %+v %v patches=%d", next, err, mailbox.patches)
	}
}

func TestAddCategoryRejectsReadOnlyBeforeWrite(t *testing.T) {
	mailbox := newLabelMailbox(t)
	client := mailbox.client(t)
	client.ReadOnly = true
	if _, err := AddCategory(context.Background(), client, "pod@example.invalid", "immutable-before", "Selected"); err == nil || mailbox.patches != 0 {
		t.Fatalf("err=%v patches=%d", err, mailbox.patches)
	}
}
