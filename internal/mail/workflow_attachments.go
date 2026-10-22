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
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/yourname/o365-cli/internal/graph"
)

const maxPDFBytes = 20 << 20
const maxPDFText = 30000

// SavedPDF is one PDF attachment stored under its content hash, with its extracted text for model-based reading.
type SavedPDF struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Size          int    `json:"size"`
	Text          string `json:"text"`
	TextTruncated bool   `json:"textTruncated"`
	TextSource    string `json:"textSource"` // pdf, ocr or none
	TextError     string `json:"textError,omitempty"`
}

// SaveMessagePDFs stores every non-inline PDF attachment of one message as DIR/<sha256>.pdf and returns its text.
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
			saved.Text, saved.TextTruncated, saved.TextError = pdfText(content)
			saved.TextSource = "pdf"
			// Scans carry no text layer: render the first pages and read them with tesseract.
			if len(strings.TrimSpace(saved.Text)) < 50 {
				text, ocrErr := ocr(ctx, content)
				saved.Text, saved.TextTruncated, saved.TextSource, saved.TextError = text, false, "ocr", ocrErr
				if runes := []rune(text); len(runes) > maxPDFText {
					saved.Text, saved.TextTruncated = string(runes[:maxPDFText]), true
				}
				if strings.TrimSpace(saved.Text) == "" {
					saved.TextSource = "none"
				}
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

// pdfText extracts plain text; malformed PDFs report an error instead of failing the whole message.
func pdfText(content []byte) (text string, truncated bool, failure string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text, truncated, failure = "", false, "text extraction failed"
		}
	}()
	document, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", false, "text extraction failed"
	}
	reader, err := document.GetPlainText()
	if err != nil {
		return "", false, "text extraction failed"
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxPDFText*4))
	if err != nil {
		return "", false, "text extraction failed"
	}
	runes := []rune(strings.TrimSpace(string(raw)))
	if len(runes) > maxPDFText {
		return string(runes[:maxPDFText]), true, ""
	}
	return string(runes), false, ""
}

// ocr is replaceable in tests so they do not need tesseract.
var ocr = ocrText

// ocrText renders up to five pages with pdftoppm and reads them with tesseract (German and English).
func ocrText(ctx context.Context, content []byte) (string, string) {
	pdftoppm, tesseract := tool("pdftoppm"), tool("tesseract")
	if pdftoppm == "" || tesseract == "" {
		return "", "OCR tools pdftoppm and tesseract are not installed"
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	work, err := os.MkdirTemp("", "o365-ocr-")
	if err != nil {
		return "", "OCR failed"
	}
	defer os.RemoveAll(work)
	input := filepath.Join(work, "in.pdf")
	if os.WriteFile(input, content, 0600) != nil || exec.CommandContext(ctx, pdftoppm, "-r", "200", "-f", "1", "-l", "5", "-png", input, filepath.Join(work, "page")).Run() != nil {
		return "", "OCR failed to render the PDF"
	}
	pages, _ := filepath.Glob(filepath.Join(work, "page*.png"))
	sort.Strings(pages)
	var text strings.Builder
	for _, page := range pages {
		out, err := exec.CommandContext(ctx, tesseract, page, "-", "-l", "deu+eng").Output()
		if err != nil {
			return "", "OCR failed to read a page"
		}
		text.Write(out)
		text.WriteString("\n")
	}
	return strings.TrimSpace(text.String()), ""
}

// tool finds an OCR helper on PATH or in the Homebrew prefix, since Pods start programs with a minimal PATH.
func tool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return filepath.Join(dir, name)
		}
	}
	return ""
}
