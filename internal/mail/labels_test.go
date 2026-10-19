package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

const labelFixture = `{"id":"immutable-before","changeKey":"v1","parentFolderId":"inbox-id","isRead":false,"isDraft":false,"categories":["Unrelated"],"conversationId":"conversation-1","from":{"emailAddress":{"address":"sender@example.invalid"}},"subject":"Subject","body":{"content":"Complete body"}}`

type labelMailbox struct {
	sync.Mutex
	raw       map[string]any
	patches   int
	mode      string
	blockRead chan struct{}
	entered   chan struct{}
}

func newLabelMailbox(t *testing.T) *labelMailbox {
	t.Helper()
	mailbox := &labelMailbox{}
	if err := json.Unmarshal([]byte(labelFixture), &mailbox.raw); err != nil {
		t.Fatal(err)
	}
	return mailbox
}
func (m *labelMailbox) client(t *testing.T) *graph.Client {
	t.Helper()
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/users/pod@example.invalid/") {
			t.Errorf("unexpected account path %s", r.URL.Path)
			return nil, errors.New("wrong account")
		}
		if strings.Contains(r.URL.Path, "/mailFolders/") {
			return fixtureResponse(`{"id":"inbox-id"}`), nil
		}
		if r.Method == http.MethodGet && m.blockRead != nil {
			select {
			case m.entered <- struct{}{}:
			default:
			}
			<-m.blockRead
		}
		m.Lock()
		defer m.Unlock()
		if r.Method == http.MethodPatch {
			m.patches++
			if r.Header.Get("Prefer") != `IdType="ImmutableId"` {
				t.Error("missing immutable identity")
			}
			var body struct {
				Categories []string `json:"categories"`
			}
			raw, _ := io.ReadAll(r.Body)
			if json.Unmarshal(raw, &body) != nil {
				t.Error("invalid PATCH")
			}
			if m.mode == "refused" {
				response := fixtureResponse(`{}`)
				response.StatusCode = 412
				return response, nil
			}
			if m.mode != "not-written" {
				m.raw["categories"] = body.Categories
				m.raw["changeKey"] = "v2"
			}
			if m.mode == "extra-label" {
				m.raw["categories"] = append(body.Categories, "External")
			}
			if m.mode == "timeout-applied" || m.mode == "not-written" {
				return nil, errors.New("synthetic lost response")
			}
			if m.mode == "readback-fails" {
				return fixtureResponse(m.json()), nil
			}
			return fixtureResponse(m.json()), nil
		}
		if m.mode == "readback-fails" && m.patches > 0 {
			return nil, errors.New("synthetic readback unavailable")
		}
		return fixtureResponse(m.json()), nil
	})
	return client
}
func (m *labelMailbox) json() string { raw, _ := json.Marshal(m.raw); return string(raw) }
func labelRequest(t *testing.T) LabelRequest {
	t.Helper()
	version, err := AssessmentContentVersion(json.RawMessage(labelFixture))
	if err != nil {
		t.Fatal(err)
	}
	return LabelRequest{Account: "pod@example.invalid", Message: "immutable-before", ContentVersion: version, OperationID: "decision-1", Dimension: []string{"Topic A", "Topic B"}, Category: "Topic A", Gate: LabelGate{RequireUnread: true, Exclusive: [][]string{{"Topic A", "Topic B"}}}}
}

func TestLabelFreshMergeAndIdempotence(t *testing.T) {
	mailbox := newLabelMailbox(t)
	client := mailbox.client(t)
	root := archiveDirectory(t)
	request := labelRequest(t)
	for i := 0; i < 2; i++ {
		receipt, err := DecideLabel(context.Background(), client, root, request)
		if err != nil || receipt.Outcome != "confirmed" || !sameSet(receipt.Categories, []string{"Unrelated", "Topic A"}) {
			t.Fatalf("bad receipt %+v %v", receipt, err)
		}
	}
	request.OperationID = "new-policy-version"
	receipt, err := DecideLabel(context.Background(), client, root, request)
	if err != nil || receipt.Outcome != "skipped" || mailbox.patches != 1 {
		t.Fatalf("existing decision changed %+v %v writes=%d", receipt, err, mailbox.patches)
	}
}

func TestLabelFreshEligibilityAndContent(t *testing.T) {
	for _, scenario := range []string{"read", "moved", "changed-body", "owned", "conflict", "prerequisite", "veto", "group", "missing-isRead", "missing-categories", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			mailbox := newLabelMailbox(t)
			request := labelRequest(t)
			expected := "skipped"
			switch scenario {
			case "read":
				mailbox.raw["isRead"] = true
			case "moved":
				mailbox.raw["parentFolderId"] = "elsewhere"
			case "changed-body":
				mailbox.raw["body"] = map[string]any{"content": "New body"}
				expected = "deferred"
			case "owned":
				mailbox.raw["categories"] = []string{"Topic B", "Unrelated"}
			case "conflict":
				mailbox.raw["categories"] = []string{"Topic A", "Topic B"}
				expected = "conflict"
			case "prerequisite":
				request.Gate.RequireAll = []string{"Missing"}
			case "veto":
				request.Gate.Forbid = []string{"Unrelated"}
			case "group":
				request.Gate.RequireAny = [][]string{{"Missing", "Also missing"}}
			case "missing-isRead":
				delete(mailbox.raw, "isRead")
				expected = "notApplied"
			case "missing-categories":
				delete(mailbox.raw, "categories")
				expected = "notApplied"
			case "truncated":
				mailbox.raw["bodyTruncated"] = true
				expected = "notApplied"
			}
			receipt, err := DecideLabel(context.Background(), mailbox.client(t), archiveDirectory(t), request)
			if err != nil || receipt.Outcome != expected || mailbox.patches != 0 {
				t.Fatalf("bad gate %+v %v writes=%d", receipt, err, mailbox.patches)
			}
		})
	}
}

func TestLabelUnknownReadbackAndImmutableReconciliation(t *testing.T) {
	for _, mode := range []string{"not-written", "extra-label", "readback-fails", "timeout-applied", "refused"} {
		t.Run(mode, func(t *testing.T) {
			mailbox := newLabelMailbox(t)
			mailbox.mode = mode
			request := labelRequest(t)
			root := archiveDirectory(t)
			client := mailbox.client(t)
			receipt, err := DecideLabel(context.Background(), client, root, request)
			expected := "unknown"
			if mode == "timeout-applied" {
				expected = "confirmed"
			}
			if mode == "refused" {
				expected = "notApplied"
			}
			if err != nil || receipt.Outcome != expected || mailbox.patches != 1 {
				t.Fatalf("bad outcome %+v %v", receipt, err)
			}
			again, err := DecideLabel(context.Background(), client, root, request)
			if err != nil || again.Outcome != expected || mailbox.patches != 1 {
				t.Fatalf("operation replayed %+v %v", again, err)
			}
			request.OperationID = "different-pod"
			if expected == "unknown" {
				if _, err := DecideLabel(context.Background(), client, root, request); err == nil {
					t.Fatal("unknown write replayed under new identity")
				}
				if _, err := ProtectedArchive(context.Background(), client, root, archiveReview(t)); err == nil {
					t.Fatal("archive bypassed unknown label hold")
				}
				records, err := InspectLabels(root, request.Account)
				if err != nil || len(records) != 1 {
					t.Fatal(records, err)
				}
				resolution := LabelResolution{Account: request.Account, Message: request.Message, OperationID: "decision-1", ReceiptSHA256: records[0]["sha256"].(string), Outcome: "notApplied", Evidence: "Synthetic provider audit proved no application"}
				invalid := resolution
				invalid.ReceiptSHA256 = strings.Repeat("a", 64)
				if ReconcileLabel(root, invalid) == nil {
					t.Fatal("unbound observation accepted")
				}
				if err := ReconcileLabel(root, resolution); err != nil {
					t.Fatal(err)
				}
				if err := ReconcileLabel(root, resolution); err != nil {
					t.Fatal(err)
				}
				changed := resolution
				changed.Outcome = "confirmed"
				if ReconcileLabel(root, changed) == nil {
					t.Fatal("observation overwritten")
				}
				request.OperationID = "decision-1"
				resolved, err := DecideLabel(context.Background(), client, root, request)
				if err != nil || resolved.Outcome != "notApplied" || mailbox.patches != 1 {
					t.Fatal("old operation replayed after reconciliation", resolved, err)
				}
			}
		})
	}
}

func TestLabelWritersShareArchiveLockAndPreserveOtherDimensions(t *testing.T) {
	mailbox := newLabelMailbox(t)
	mailbox.blockRead = make(chan struct{})
	mailbox.entered = make(chan struct{}, 1)
	client := mailbox.client(t)
	root := archiveDirectory(t)
	first := labelRequest(t)
	second := first
	second.OperationID = "reply-1"
	second.Dimension = []string{"Reply", "No reply"}
	second.Category = "No reply"
	type result struct {
		receipt *LabelReceipt
		err     error
	}
	done := make(chan result, 1)
	go func() {
		receipt, err := DecideLabel(context.Background(), client, root, first)
		done <- result{receipt, err}
	}()
	<-mailbox.entered
	if receipt, err := DecideLabel(context.Background(), client, root, second); err != nil || receipt.Outcome != "busy" {
		t.Fatal("overlapping label writer did not return busy", err)
	}
	if receipt, err := ProtectedArchive(context.Background(), client, root, archiveReview(t)); err != nil || receipt.Outcome != "notApplied" || !strings.Contains(receipt.Reason, "busy") {
		t.Fatal("archive did not share the label lock", err)
	}
	close(mailbox.blockRead)
	completed := <-done
	if completed.err != nil || completed.receipt.Outcome != "confirmed" {
		t.Fatal(completed)
	}
	receipt, err := DecideLabel(context.Background(), client, root, second)
	if err != nil || receipt.Outcome != "confirmed" || !sameSet(receipt.Categories, []string{"Unrelated", "Topic A", "No reply"}) || mailbox.patches != 2 {
		t.Fatalf("lost concurrent categories %+v %v", receipt, err)
	}
}

func TestLabelAccountIsolationAndUncertainArchiveHold(t *testing.T) {
	root := archiveDirectory(t)
	journal, err := openArchiveJournal(root, "other@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.close()
	mailbox := newLabelMailbox(t)
	receipt, err := DecideLabel(context.Background(), mailbox.client(t), root, labelRequest(t))
	if err != nil || receipt.Outcome != "confirmed" {
		t.Fatalf("other account locked this mailbox %+v %v", receipt, err)
	}
	request := labelRequest(t)
	request.Account = "other@example.invalid"
	if receipt, err := DecideLabel(context.Background(), mailbox.client(t), root, request); err != nil || receipt.Outcome != "busy" {
		t.Fatal("same account did not lock")
	}
	journal.close()
	own, err := openArchiveJournal(root, "pod@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := own.save(&ArchiveReceipt{Protocol: "o365-archive/v1", Request: archiveReview(t), Outcome: "unknown"}); err != nil {
		t.Fatal(err)
	}
	own.close()
	request = labelRequest(t)
	request.OperationID = "new-operation"
	if _, err := DecideLabel(context.Background(), mailbox.client(t), root, request); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatal("label writer bypassed archive uncertainty", err)
	}
}

func TestLabelCrashIntentAndOSLockRelease(t *testing.T) {
	if os.Getenv("O365_LABEL_LOCK_HELPER") == "1" {
		root := os.Getenv("O365_LABEL_TEST_ROOT")
		journal, err := openArchiveJournal(root, "pod@example.invalid")
		if err != nil {
			os.Exit(2)
		}
		request := labelRequest(t)
		receipt := &LabelReceipt{Protocol: "o365-labels/v1", Request: request, Outcome: "unknown", Before: []string{"Unrelated"}, Categories: []string{"Unrelated", "Topic A"}}
		if journal.saveLabel(receipt) != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	root := archiveDirectory(t)
	process := exec.Command(os.Args[0], "-test.run=^TestLabelCrashIntentAndOSLockRelease$")
	process.Env = append(os.Environ(), "O365_LABEL_LOCK_HELPER=1", "O365_LABEL_TEST_ROOT="+root)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("helper failed %s %v", output, err)
	}
	mailbox := newLabelMailbox(t)
	receipt, err := DecideLabel(context.Background(), mailbox.client(t), root, labelRequest(t))
	if err != nil || receipt.Outcome != "unknown" || mailbox.patches != 0 {
		t.Fatalf("crash intent replayed or lock stuck %+v %v", receipt, err)
	}
}

func TestProtectedArchiveFreshGenericLabelGate(t *testing.T) {
	for _, scenario := range []string{"read", "veto", "missing", "conflict", "moved", "eligible"} {
		t.Run(scenario, func(t *testing.T) {
			raw := strings.TrimSuffix(archiveFixture, "}") + `,"isRead":false,"categories":["No reply","Archive proposal"]}`
			gate := &LabelGate{RequireUnread: true, RequireAll: []string{"No reply", "Archive proposal"}, Forbid: []string{"Keep", "Reply"}, Exclusive: [][]string{{"Reply", "No reply"}, {"Archive proposal", "Remain"}}}
			switch scenario {
			case "read":
				raw = strings.Replace(raw, `"isRead":false`, `"isRead":true`, 1)
			case "veto":
				raw = strings.Replace(raw, `"categories":[`, `"categories":["Keep",`, 1)
			case "missing":
				raw = strings.Replace(raw, `"No reply",`, "", 1)
			case "conflict":
				raw = strings.Replace(raw, `"categories":[`, `"categories":["Remain",`, 1)
			case "moved":
				raw = strings.Replace(raw, "inbox-id", "elsewhere", 1)
			}
			request := archiveReview(t)
			request.Gate = gate
			client, count := archiveFixtureClient(t, raw, "confirmed")
			receipt, err := ProtectedArchive(context.Background(), client, archiveDirectory(t), request)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "eligible" {
				if receipt.Outcome != "archived" || *count != 1 {
					t.Fatal(receipt, *count)
				}
			} else if receipt.Outcome != "notApplied" || *count != 0 {
				t.Fatal("stale eligibility moved", receipt, *count)
			}
		})
	}
}
