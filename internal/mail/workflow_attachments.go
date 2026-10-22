package mail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

const maxPDFBytes = 20 << 20

// SavedPDF is one PDF attachment stored under its content hash; reading its text is the job of a separate PDF tool.
type SavedPDF struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// SaveMessagePDFs stores every non-inline PDF attachment of one message as DIR/<sha256>.pdf.
// Repeats reuse identical files; a different file under the same name is refused.
func SaveMessagePDFs(ctx context.Context, client *graph.Client, account, message, dir string) (*WorkflowReply, error) {
	if err := (WorkflowRequest{Operation: "read", Account: account, Message: message}).Validate(); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || !filepath.IsAbs(dir) {
		return nil, errors.New("--save-to must be an existing absolute directory, not a symlink")
	}
	reply := &WorkflowReply{Protocol: "pods-mail/v1", Account: account, Operation: "attachments", Outcome: "confirmed", Items: []json.RawMessage{}}
	endpoint := graph.GraphAPIBaseURL + "/users/" + url.PathEscape(account) + "/messages/" + url.PathEscape(message) + "/attachments"
	for pages := 0; endpoint != ""; pages++ {
		if pages >= 10 {
			return nil, errors.New("attachment pagination did not terminate")
		}
		body, err := client.DoRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Value []struct {
				Type         string `json:"@odata.type"`
				Name         string `json:"name"`
				ContentType  string `json:"contentType"`
				IsInline     bool   `json:"isInline"`
				Size         int    `json:"size"`
				ContentBytes string `json:"contentBytes"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if json.Unmarshal(body, &page) != nil {
			return nil, errors.New("Graph returned an unreadable attachment list")
		}
		for _, a := range page.Value {
			isPDF := strings.EqualFold(a.ContentType, "application/pdf") || strings.HasSuffix(strings.ToLower(a.Name), ".pdf")
			if a.Type != "#microsoft.graph.fileAttachment" || a.IsInline || !isPDF {
				continue
			}
			if a.Size > maxPDFBytes {
				return nil, fmt.Errorf("attachment %q exceeds 20 MiB", a.Name)
			}
			content, err := base64.StdEncoding.DecodeString(a.ContentBytes)
			if err != nil || !bytes.HasPrefix(content, []byte("%PDF-")) {
				return nil, fmt.Errorf("attachment %q is not a readable PDF", a.Name)
			}
			sum := sha256.Sum256(content)
			saved := SavedPDF{Name: a.Name, SHA256: hex.EncodeToString(sum[:]), Size: len(content)}
			saved.Path = filepath.Join(dir, saved.SHA256+".pdf")
			if err := writeOnce(saved.Path, content); err != nil {
				return nil, err
			}
			item, _ := json.Marshal(saved)
			reply.Items = append(reply.Items, item)
		}
		endpoint = page.Next
		if endpoint != "" {
			if err := graph.ValidateEndpoint(endpoint); err != nil {
				return nil, err
			}
		}
	}
	return reply, nil
}

// writeOnce creates the file, or accepts an existing file with identical content.
func writeOnce(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, content) {
			return fmt.Errorf("%s exists with different content", filepath.Base(path))
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
