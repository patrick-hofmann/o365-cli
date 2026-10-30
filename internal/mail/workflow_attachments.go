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
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
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
	base := graph.GraphAPIBaseURL + "/users/" + url.PathEscape(account) + "/messages/" + url.PathEscape(message) + "/attachments"
	// Without $select Graph inlines every attachment's contentBytes, which breaks the response cap on large mails.
	endpoint := base + "?$select=id,name,contentType,isInline,size"
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
				Type        string `json:"@odata.type"`
				ID          string `json:"id"`
				Name        string `json:"name"`
				ContentType string `json:"contentType"`
				IsInline    bool   `json:"isInline"`
				Size        int    `json:"size"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if json.Unmarshal(body, &page) != nil {
			return nil, errors.New("Graph returned an unreadable attachment list")
		}
		for _, a := range page.Value {
			isPDF := strings.EqualFold(a.ContentType, "application/pdf") || strings.HasSuffix(strings.ToLower(a.Name), ".pdf")
			// Clear-signed S/MIME mails expose their whole MIME body as one smime.p7m attachment.
			isSMIME := strings.EqualFold(a.ContentType, "multipart/signed") || strings.EqualFold(a.ContentType, "application/pkcs7-mime") || strings.EqualFold(a.Name, "smime.p7m")
			if a.Type != "#microsoft.graph.fileAttachment" || a.IsInline || !(isPDF || isSMIME) {
				continue
			}
			if a.Size > maxPDFBytes && !isSMIME {
				return nil, fmt.Errorf("attachment %q exceeds 20 MiB", a.Name)
			}
			content, err := client.DoRequest(http.MethodGet, base+"/"+url.PathEscape(a.ID)+"/$value", nil)
			if err != nil {
				return nil, err
			}
			pdfs := []namedPDF{{a.Name, content}}
			if !isPDF {
				if pdfs, err = signedPDFs(content); err != nil {
					return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
				}
			}
			for _, pdf := range pdfs {
				if !bytes.HasPrefix(pdf.content, []byte("%PDF-")) {
					return nil, fmt.Errorf("attachment %q is not a readable PDF", pdf.name)
				}
				sum := sha256.Sum256(pdf.content)
				saved := SavedPDF{Name: pdf.name, SHA256: hex.EncodeToString(sum[:]), Size: len(pdf.content)}
				saved.Path = filepath.Join(dir, saved.SHA256+".pdf")
				if err := writeOnce(saved.Path, pdf.content); err != nil {
					return nil, err
				}
				item, _ := json.Marshal(saved)
				reply.Items = append(reply.Items, item)
			}
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

type namedPDF struct {
	name    string
	content []byte
}

// signedPDFs returns the non-inline PDF parts of a clear-signed (multipart/signed) MIME entity.
// The signature is not verified; opaque-signed or encrypted S/MIME is refused.
func signedPDFs(entity []byte) ([]namedPDF, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(entity))
	if err != nil {
		return nil, errors.New("S/MIME content is not readable MIME")
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/signed" {
		return nil, fmt.Errorf("unsupported S/MIME content %q; only clear-signed multipart/signed is supported", mediaType)
	}
	var pdfs []namedPDF
	err = walkMIME(multipart.NewReader(msg.Body, params["boundary"]), 0, &pdfs)
	return pdfs, err
}

func walkMIME(reader *multipart.Reader, depth int, pdfs *[]namedPDF) error {
	if depth > 5 {
		return errors.New("S/MIME content nests too deep")
	}
	for {
		part, err := reader.NextRawPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("S/MIME content has a broken multipart structure")
		}
		mediaType, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disposition, dispParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := dispParams["filename"]
		if name == "" {
			name = params["name"]
		}
		if decoded, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
			name = decoded
		}
		switch {
		case strings.HasPrefix(mediaType, "multipart/"):
			if err := walkMIME(multipart.NewReader(part, params["boundary"]), depth+1, pdfs); err != nil {
				return err
			}
		case disposition != "inline" && (mediaType == "application/pdf" || strings.HasSuffix(strings.ToLower(name), ".pdf")):
			var body io.Reader = part
			switch strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Transfer-Encoding"))) {
			case "base64":
				body = base64.NewDecoder(base64.StdEncoding, part)
			case "quoted-printable":
				body = quotedprintable.NewReader(part)
			}
			content, err := io.ReadAll(io.LimitReader(body, maxPDFBytes+1))
			if err != nil {
				return fmt.Errorf("PDF %q cannot be decoded", name)
			}
			if len(content) > maxPDFBytes {
				return fmt.Errorf("PDF %q exceeds 20 MiB", name)
			}
			*pdfs = append(*pdfs, namedPDF{filepath.Base(name), content})
		}
	}
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
