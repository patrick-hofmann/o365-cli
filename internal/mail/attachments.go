package mail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourname/o365-cli/internal/graph"
)

// attachmentInlineLimit is the threshold (≤ 3 MB) below which Microsoft Graph
// accepts a single POST with base64 contentBytes. Larger files must use
// createUploadSession + chunked PUTs.
const attachmentInlineLimit = 3 * 1024 * 1024

// attachmentChunkSize is the chunk size for upload-session PUTs.
// Graph requires chunks to be a multiple of 320 KiB except the last; 4 MiB
// (4194304 = 13 × 320 KiB × 1.0078) is the recommended size — we round to
// 4194304 / 327680 * 327680 to stay on grid.
const attachmentChunkSize = 4 * 1024 * 1024 // 4 MiB, multiple of 320 KiB

// AddAttachment uploads a file as attachment to an existing message (typically
// a draft). Auto-selects between inline POST (≤ 3 MB) and createUploadSession
// (> 3 MB, chunked PUT). Returns the attachment ID.
func (c *Client) AddAttachment(messageID, filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", filePath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("attachment %q is a directory", filePath)
	}

	name := filepath.Base(filePath)
	contentType := detectAttachmentContentType(name)

	if info.Size() <= attachmentInlineLimit {
		return c.addAttachmentInline(messageID, name, contentType, filePath)
	}
	return c.addAttachmentUploadSession(messageID, name, contentType, filePath, info.Size())
}

// addAttachmentInline handles files ≤ 3 MB via single POST with base64 body.
func (c *Client) addAttachmentInline(messageID, name, contentType, filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read attachment %q: %w", filePath, err)
	}

	body := map[string]interface{}{
		"@odata.type":  "#microsoft.graph.fileAttachment",
		"name":         name,
		"contentType":  contentType,
		"contentBytes": base64.StdEncoding.EncodeToString(data),
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal attachment payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/me/messages/%s/attachments",
		graph.GraphAPIBaseURL, url.PathEscape(messageID))
	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return "", fmt.Errorf("upload inline attachment %q: %w", name, err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse attachment response: %w", err)
	}
	return result.ID, nil
}

// addAttachmentUploadSession handles files > 3 MB via createUploadSession +
// chunked PUTs. The returned uploadUrl is pre-authenticated; we MUST NOT add
// Authorization headers on the PUT requests.
func (c *Client) addAttachmentUploadSession(messageID, name, contentType, filePath string, size int64) (string, error) {
	sessionBody := map[string]interface{}{
		"AttachmentItem": map[string]interface{}{
			"attachmentType": "file",
			"name":           name,
			"size":           size,
			"contentType":    contentType,
		},
	}
	jsonBody, err := json.Marshal(sessionBody)
	if err != nil {
		return "", fmt.Errorf("marshal upload-session payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/me/messages/%s/attachments/createUploadSession",
		graph.GraphAPIBaseURL, url.PathEscape(messageID))
	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return "", fmt.Errorf("create upload session for %q: %w", name, err)
	}

	var session struct {
		UploadURL          string `json:"uploadUrl"`
		ExpirationDateTime string `json:"expirationDateTime"`
	}
	if err := json.Unmarshal(resp, &session); err != nil {
		return "", fmt.Errorf("parse upload-session response: %w", err)
	}
	if session.UploadURL == "" {
		return "", fmt.Errorf("upload session response missing uploadUrl")
	}

	return c.streamChunksToUploadURL(session.UploadURL, filePath, size)
}

// streamChunksToUploadURL streams the file in chunks to the pre-signed upload URL.
// The final PUT response contains the attachment metadata (or a Location header).
func (c *Client) streamChunksToUploadURL(uploadURL, filePath string, size int64) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open attachment %q: %w", filePath, err)
	}
	defer f.Close()

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	buf := make([]byte, attachmentChunkSize)

	var offset int64
	for offset < size {
		n, readErr := io.ReadFull(f, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return "", fmt.Errorf("read chunk at offset %d: %w", offset, readErr)
		}
		if n == 0 {
			break
		}
		chunk := buf[:n]
		end := offset + int64(n) - 1
		rangeHeader := fmt.Sprintf("bytes %d-%d/%d", offset, end, size)

		req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(chunk))
		if err != nil {
			return "", fmt.Errorf("build PUT request: %w", err)
		}
		req.Header.Set("Content-Length", fmt.Sprintf("%d", n))
		req.Header.Set("Content-Range", rangeHeader)
		// No Authorization header — uploadUrl is pre-signed.

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("PUT chunk %s: %w", rangeHeader, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("PUT chunk %s failed (status %d): %s",
				rangeHeader, resp.StatusCode, string(respBody))
		}

		isLastChunk := offset+int64(n) >= size
		if isLastChunk {
			// Final chunk: response should be 201 Created with attachment in body
			// (Graph behaviour) or a Location header.
			var att struct {
				ID string `json:"id"`
			}
			if jsonErr := json.Unmarshal(respBody, &att); jsonErr == nil && att.ID != "" {
				return att.ID, nil
			}
			if loc := resp.Header.Get("Location"); loc != "" {
				// Location is the absolute URL of the new attachment; extract ID.
				if idx := strings.LastIndex(loc, "/"); idx != -1 {
					return loc[idx+1:], nil
				}
				return loc, nil
			}
			// Upload succeeded but no ID — return empty string, caller can ignore.
			return "", nil
		}
		offset += int64(n)
	}
	return "", fmt.Errorf("upload session ended without final chunk for size %d", size)
}

// detectAttachmentContentType returns a best-effort MIME type for the given
// filename. Uses mime.TypeByExtension first; falls back to explicit overrides
// for common Office types (which are not always in Go's default mime DB) and
// finally to application/octet-stream.
func detectAttachmentContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ct := mime.TypeByExtension(ext); ct != "" {
		// Strip "; charset=..." if present.
		if idx := strings.Index(ct, ";"); idx != -1 {
			ct = strings.TrimSpace(ct[:idx])
		}
		return ct
	}
	switch ext {
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return "application/msword"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".csv":
		return "text/csv"
	}
	return "application/octet-stream"
}
