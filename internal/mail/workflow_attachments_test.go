package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func TestSaveMessagePDFsStoresPDFsOnce(t *testing.T) {
	scan := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 scanned page without text layer"))
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1.0/users/pod@example.invalid/messages/AAkALgAAAAAAHYQDEapmEc2byACqAC-EWg0Aihx0qtzv3k_XDmEEfSdYlgAJPeoZKgAA/attachments" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return fixtureResponse(`{"value":[
			{"@odata.type":"#microsoft.graph.fileAttachment","name":"Rechnung.pdf","contentType":"application/pdf","size":40,"contentBytes":"` + scan + `"},
			{"@odata.type":"#microsoft.graph.fileAttachment","name":"logo.png","contentType":"image/png","isInline":true,"contentBytes":"iVBORw=="},
			{"@odata.type":"#microsoft.graph.itemAttachment","name":"forwarded.pdf"}]}`), nil
	})
	dir := t.TempDir()
	message := "AAkALgAAAAAAHYQDEapmEc2byACqAC-EWg0Aihx0qtzv3k_XDmEEfSdYlgAJPeoZKgAA"
	for run := 0; run < 2; run++ { // the repeat must reuse the identical file
		reply, err := SaveMessagePDFs(context.Background(), client, "pod@example.invalid", message, dir)
		if err != nil || len(reply.Items) != 1 {
			t.Fatalf("run %d: %+v %v", run, reply, err)
		}
		var saved SavedPDF
		if json.Unmarshal(reply.Items[0], &saved) != nil || saved.Name != "Rechnung.pdf" || filepath.Base(saved.Path) != saved.SHA256+".pdf" {
			t.Fatalf("run %d item %s", run, reply.Items[0])
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("expected one stored file, got %d", len(entries))
	}
	if _, err := SaveMessagePDFs(context.Background(), client, "pod@example.invalid", message, "relative/dir"); err == nil {
		t.Fatal("relative directory accepted")
	}
}
