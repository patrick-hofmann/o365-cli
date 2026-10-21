package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func labelUpdate(remove, add []string) LabelUpdate {
	return LabelUpdate{Account: "pod@example.invalid", Message: "immutable-before", Remove: remove, Add: add}
}

func TestUpdateLabelsReplacesValueOnceAndPreservesOthers(t *testing.T) {
	mailbox := newLabelMailbox(t)
	mailbox.raw["categories"] = []string{"Unrelated", "Wissen: offen"}
	client := mailbox.client(t)
	root := archiveDirectory(t)
	update := labelUpdate([]string{"Wissen: offen", "Wissen: prüfen"}, []string{"Wissen: gesichert"})
	first, err := UpdateLabels(context.Background(), client, root, update)
	if err != nil || first.Outcome != "confirmed" || !sameSet(first.Categories, []string{"Unrelated", "Wissen: gesichert"}) {
		t.Fatalf("bad first update %+v %v", first, err)
	}
	again, err := UpdateLabels(context.Background(), client, root, update)
	if err != nil || again.Outcome != "skipped" || mailbox.patches != 1 {
		t.Fatalf("repeat wrote again %+v %v writes=%d", again, err, mailbox.patches)
	}
	version, _ := AssessmentContentVersion(json.RawMessage(labelFixture))
	update.ContentVersion = version
	mailbox.raw["body"] = map[string]any{"content": "Changed after the write"}
	mailbox.raw["parentFolderId"] = "elsewhere"
	reached, err := UpdateLabels(context.Background(), client, root, update)
	if err != nil || reached.Outcome != "skipped" || mailbox.patches != 1 {
		t.Fatalf("reached target not skipped after content change %+v %v", reached, err)
	}
}

func TestUpdateLabelsWritesSeveralChangesInOnePatch(t *testing.T) {
	mailbox := newLabelMailbox(t)
	mailbox.raw["categories"] = []string{"Unrelated", "Old A", "Kept"}
	update := labelUpdate([]string{"Old A", "Absent"}, []string{"New A", "Kept", "New B"})
	receipt, err := UpdateLabels(context.Background(), mailbox.client(t), archiveDirectory(t), update)
	if err != nil || receipt.Outcome != "confirmed" || mailbox.patches != 1 || !sameSet(receipt.Categories, []string{"Unrelated", "Kept", "New A", "New B"}) {
		t.Fatalf("bad combined update %+v %v writes=%d", receipt, err, mailbox.patches)
	}
}

func TestUpdateLabelsRefusesBeforeDispatch(t *testing.T) {
	for _, scenario := range []string{"overlap", "empty", "bound", "moved", "changed-body", "busy"} {
		t.Run(scenario, func(t *testing.T) {
			mailbox := newLabelMailbox(t)
			root := archiveDirectory(t)
			update := labelUpdate(nil, []string{"New"})
			version, _ := AssessmentContentVersion(json.RawMessage(labelFixture))
			expected := "deferred"
			switch scenario {
			case "overlap":
				update.Remove = []string{"new"}
			case "empty":
				update.Add = nil
			case "bound":
				names := []string{}
				for i := 0; i < 25; i++ {
					names = append(names, "Label "+strings.Repeat("x", i+1))
				}
				mailbox.raw["categories"] = names
				expected = "notApplied"
			case "moved":
				mailbox.raw["parentFolderId"] = "elsewhere"
			case "changed-body":
				update.ContentVersion = version
				mailbox.raw["body"] = map[string]any{"content": "New body"}
			case "busy":
				held, err := openArchiveJournal(root, update.Account)
				if err != nil {
					t.Fatal(err)
				}
				defer held.close()
				expected = "busy"
			}
			receipt, err := UpdateLabels(context.Background(), mailbox.client(t), root, update)
			if scenario == "overlap" || scenario == "empty" {
				if err == nil {
					t.Fatal("invalid update accepted")
				}
				return
			}
			if err != nil || receipt.Outcome != expected || mailbox.patches != 0 {
				t.Fatalf("not refused %+v %v writes=%d", receipt, err, mailbox.patches)
			}
		})
	}
}

func TestUpdateLabelsInterruptedWriteIsSafeToRepeat(t *testing.T) {
	for _, mode := range []string{"timeout-applied", "not-written"} {
		t.Run(mode, func(t *testing.T) {
			mailbox := newLabelMailbox(t)
			mailbox.mode = mode
			client := mailbox.client(t)
			root := archiveDirectory(t)
			update := labelUpdate(nil, []string{"Done"})
			first, err := UpdateLabels(context.Background(), client, root, update)
			expected := map[string]string{"timeout-applied": "confirmed", "not-written": "unknown"}[mode]
			if err != nil || first.Outcome != expected || mailbox.patches != 1 {
				t.Fatalf("bad first outcome %+v %v", first, err)
			}
			mailbox.mode = ""
			again, err := UpdateLabels(context.Background(), client, root, update)
			want, writes := "skipped", 1
			if mode == "not-written" {
				want, writes = "confirmed", 2
			}
			if err != nil || again.Outcome != want || mailbox.patches != writes {
				t.Fatalf("repeat after interruption %+v %v writes=%d", again, err, mailbox.patches)
			}
		})
	}
}

func TestUpdateLabelsWaitsForLockAndKeepsConcurrentChanges(t *testing.T) {
	LockWait = 5 * time.Second
	defer func() { LockWait = 0 }()
	mailbox := newLabelMailbox(t)
	mailbox.blockRead = make(chan struct{})
	mailbox.entered = make(chan struct{}, 1)
	client := mailbox.client(t)
	root := archiveDirectory(t)
	done := make(chan error, 2)
	go func() {
		receipt, err := UpdateLabels(context.Background(), client, root, labelUpdate(nil, []string{"First"}))
		if err == nil && receipt.Outcome != "confirmed" {
			err = fmt.Errorf("first %+v", receipt)
		}
		done <- err
	}()
	<-mailbox.entered
	go func() {
		reply, err := AddCategoryLocked(context.Background(), client, root, "pod@example.invalid", "immutable-before", "Second")
		if err == nil && reply.Outcome != "confirmed" {
			err = fmt.Errorf("second %+v", reply)
		}
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	close(mailbox.blockRead)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	categories := mailbox.raw["categories"].([]string)
	if !sameSet(categories, []string{"Unrelated", "First", "Second"}) || mailbox.patches != 2 {
		t.Fatalf("lost a concurrent change %v writes=%d", categories, mailbox.patches)
	}
}

func TestAssessmentVersionV2IgnoresFlagAndImportance(t *testing.T) {
	versions := func(change func(map[string]any)) (string, string) {
		var raw map[string]any
		_ = json.Unmarshal([]byte(labelFixture), &raw)
		change(raw)
		bytes, _ := json.Marshal(raw)
		v1, err1 := AssessmentContentVersion(bytes)
		v2, err2 := AssessmentContentVersionV2(bytes)
		if err1 != nil || err2 != nil || !strings.HasPrefix(v2, "assessment-v2:") {
			t.Fatal(err1, err2, v2)
		}
		return v1, v2
	}
	baseV1, baseV2 := versions(func(map[string]any) {})
	flagV1, flagV2 := versions(func(raw map[string]any) {
		raw["flag"] = map[string]any{"flagStatus": "flagged"}
		raw["importance"] = "high"
	})
	bodyV1, bodyV2 := versions(func(raw map[string]any) { raw["body"] = map[string]any{"content": "Other"} })
	labelV1, labelV2 := versions(func(raw map[string]any) { raw["categories"] = []string{"Other"}; raw["isRead"] = true })
	if flagV2 != baseV2 || flagV1 == baseV1 || bodyV1 == baseV1 || bodyV2 == baseV2 || labelV1 != baseV1 || labelV2 != baseV2 {
		t.Fatal("unexpected content version behavior")
	}
	mailbox := newLabelMailbox(t)
	mailbox.raw["flag"] = map[string]any{"flagStatus": "flagged"}
	update := labelUpdate(nil, []string{"Done"})
	update.ContentVersion = baseV2
	receipt, err := UpdateLabels(context.Background(), mailbox.client(t), archiveDirectory(t), update)
	if err != nil || receipt.Outcome != "confirmed" {
		t.Fatalf("v2 refused a flagged mail %+v %v", receipt, err)
	}
}
