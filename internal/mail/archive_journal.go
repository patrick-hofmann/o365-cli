package mail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type archiveJournal struct {
	path        string
	lock        *os.File
	entries     map[string]*ArchiveReceipt
	resolutions map[string]ArchiveResolution
	history     map[string]ArchiveHistory
}

func openArchiveJournal(root, account string) (*archiveJournal, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("archive journal must be an explicitly assigned absolute shared directory")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("archive journal needs an existing private directory")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != filepath.Clean(root) {
		return nil, errors.New("archive journal may not traverse symbolic links")
	}
	hash := sha256.Sum256([]byte(strings.ToLower(account)))
	path := filepath.Join(root, hex.EncodeToString(hash[:])+".json")
	lock, err := lockArchiveFile(path + ".lock")
	if err != nil {
		return nil, err
	}
	journal := &archiveJournal{path: path, lock: lock, entries: map[string]*ArchiveReceipt{}, history: map[string]ArchiveHistory{}, resolutions: map[string]ArchiveResolution{}}
	fail := func(err error) (*archiveJournal, error) { journal.close(); return nil, err }
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return fail(errors.New("archive journal is not a private regular file"))
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return journal, nil
	}
	if err != nil {
		return fail(err)
	}
	defer file.Close()
	bytes, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil {
		return fail(err)
	}
	if len(bytes) > 16<<20 {
		return fail(errors.New("archive journal requires maintenance before more writes"))
	}
	var stored struct {
		Version     int                          `json:"version"`
		Entries     map[string]*ArchiveReceipt   `json:"entries"`
		History     map[string]ArchiveHistory    `json:"history,omitempty"`
		Resolutions map[string]ArchiveResolution `json:"resolutions,omitempty"`
	}
	if json.Unmarshal(bytes, &stored) != nil || (stored.Version != 1 && stored.Version != 2 && stored.Version != 3) || stored.Entries == nil {
		return fail(errors.New("invalid archive journal; manual reconciliation required"))
	}
	for key, receipt := range stored.Entries {
		if receipt == nil || receipt.Protocol != "o365-archive/v1" || receipt.Request.Validate() != nil || key != receipt.Request.OperationID || !strings.EqualFold(receipt.Request.Account, account) || (receipt.Outcome != "unknown" && receipt.Outcome != "archived" && receipt.Outcome != "notApplied") {
			return fail(errors.New("invalid archive journal receipt"))
		}
	}
	for key, record := range stored.History {
		if record.validate(account) != nil || key != historyKey(record) {
			return fail(errors.New("invalid archive migration history"))
		}
	}
	journal.entries = stored.Entries
	if stored.History != nil {
		journal.history = stored.History
	}
	for key, r := range stored.Resolutions {
		if key != resolutionKey(r.Source, r.OperationID) || !strings.EqualFold(r.Account, account) {
			return fail(errors.New("invalid reconciliation identity"))
		}
		if err := journal.validateResolution(r); err != nil {
			return fail(err)
		}
		journal.resolutions[key] = r
	}
	return journal, nil
}

func (j *archiveJournal) close() { _ = j.lock.Close() }
func (j *archiveJournal) previous(request ArchiveRequest) (*ArchiveReceipt, error) {
	for _, record := range j.history {
		if record.Message == request.Message && (record.State == "consumed" || record.State == "unknown") && j.resolutions[resolutionKey(record.Source, record.OperationID)].Outcome != "notApplied" {
			return nil, errors.New("historical consumed or uncertain archive work holds this message; inspect its migration record")
		}
	}
	if previous, ok := j.entries[request.OperationID]; ok {
		if previous.Request != request {
			return nil, errors.New("operation id belongs to a different reviewed request")
		}
		return j.resolvedReceipt(previous), nil
	}
	for _, original := range j.entries {
		receipt := j.resolvedReceipt(original)
		if receipt.Request.Message != request.Message {
			continue
		}
		if receipt.Outcome == "unknown" {
			return nil, errors.New("an earlier archive attempt of this message awaits reconciliation")
		}
		if receipt.Outcome == "archived" && receipt.Request.ContentVersion == request.ContentVersion {
			return receipt, nil
		}
	}
	return nil, nil
}
func (j *archiveJournal) save(receipt *ArchiveReceipt) error {
	j.entries[receipt.Request.OperationID] = receipt
	return j.persist()
}
func (j *archiveJournal) persist() error {
	body, err := json.Marshal(struct {
		Version     int                          `json:"version"`
		Entries     map[string]*ArchiveReceipt   `json:"entries"`
		History     map[string]ArchiveHistory    `json:"history,omitempty"`
		Resolutions map[string]ArchiveResolution `json:"resolutions,omitempty"`
	}{3, j.entries, j.history, j.resolutions})
	if err != nil {
		return err
	}
	if len(body) > 16<<20 {
		return errors.New("archive journal exceeds its size bound")
	}
	file, err := os.CreateTemp(filepath.Dir(j.path), ".archive-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), j.path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(j.path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
