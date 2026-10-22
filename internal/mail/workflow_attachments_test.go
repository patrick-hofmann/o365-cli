package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func TestSaveMessagePDFsStoresPDFsOnce(t *testing.T) {
	const base = "/v1.0/users/pod@example.invalid/messages/AAkALgAAAAAAHYQDEapmEc2byACqAC-EWg0Aihx0qtzv3k_XDmEEfSdYlgAJPeoZKgAA/attachments"
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case base:
			// The list must never carry contentBytes: a 40 MiB video would break the 32 MiB response cap.
			if sel := r.URL.Query().Get("$select"); sel != "id,name,contentType,isInline,size" {
				t.Fatalf("list $select = %q", sel)
			}
			return fixtureResponse(`{"value":[
				{"@odata.type":"#microsoft.graph.fileAttachment","id":"pdf-1","name":"Rechnung.pdf","contentType":"application/pdf","size":40},
				{"@odata.type":"#microsoft.graph.fileAttachment","id":"video-1","name":"Boards.mp4","contentType":"video/mp4","size":41943040},
				{"@odata.type":"#microsoft.graph.fileAttachment","id":"logo-1","name":"logo.png","contentType":"image/png","isInline":true,"size":4},
				{"@odata.type":"#microsoft.graph.itemAttachment","id":"item-1","name":"forwarded.pdf"}]}`), nil
		case base + "/pdf-1/$value":
			return fixtureResponse("%PDF-1.4 scanned page without text layer"), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
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
