package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func TestSaveMessagePDFsExtractsPDFsFromClearSignedSMIME(t *testing.T) {
	const base = "/v1.0/users/pod@example.invalid/messages/signed-1/attachments"
	pdf := "%PDF-1.4 signed invoice"
	signed := "Content-Type: multipart/signed; protocol=\"application/pkcs7-signature\"; micalg=sha-256; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\nContent-Type: multipart/mixed; boundary=\"inner\"\r\n\r\n" +
		"--inner\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<p>Rechnung</p>\r\n" +
		"--inner\r\nContent-Type: application/pdf; name=\"Rechnung_1.pdf\"\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"Rechnung_1.pdf\"\r\n\r\n" +
		base64.StdEncoding.EncodeToString([]byte(pdf))[:16] + "\r\n" + base64.StdEncoding.EncodeToString([]byte(pdf))[16:] + "\r\n" +
		"--inner--\r\n\r\n" +
		"--outer\r\nContent-Type: application/pkcs7-signature; name=\"smime.p7s\"\r\nContent-Transfer-Encoding: base64\r\n\r\nAAAA\r\n--outer--\r\n"
	opaque := "Content-Type: application/pkcs7-mime; smime-type=enveloped-data; name=\"smime.p7m\"\r\n\r\nAAAA\r\n"
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case base:
			return fixtureResponse(`{"value":[{"@odata.type":"#microsoft.graph.fileAttachment","id":"p7m","name":"smime.p7m","contentType":"multipart/signed","size":900}]}`), nil
		case base + "/p7m/$value":
			return fixtureResponse(signed), nil
		case "/v1.0/users/pod@example.invalid/messages/opaque-1/attachments":
			return fixtureResponse(`{"value":[{"@odata.type":"#microsoft.graph.fileAttachment","id":"p7m","name":"smime.p7m","contentType":"application/pkcs7-mime","size":9}]}`), nil
		case "/v1.0/users/pod@example.invalid/messages/opaque-1/attachments/p7m/$value":
			return fixtureResponse(opaque), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})
	dir := t.TempDir()
	reply, err := SaveMessagePDFs(context.Background(), client, "pod@example.invalid", "signed-1", dir)
	if err != nil || len(reply.Items) != 1 {
		t.Fatalf("%+v %v", reply, err)
	}
	var saved SavedPDF
	if json.Unmarshal(reply.Items[0], &saved) != nil || saved.Name != "Rechnung_1.pdf" || saved.Size != len(pdf) {
		t.Fatalf("item %s", reply.Items[0])
	}
	if stored, _ := os.ReadFile(saved.Path); string(stored) != pdf {
		t.Fatalf("stored %q", stored)
	}
	if _, err := SaveMessagePDFs(context.Background(), client, "pod@example.invalid", "opaque-1", dir); err == nil || !strings.Contains(err.Error(), "clear-signed") {
		t.Fatalf("opaque S/MIME: %v", err)
	}
}
