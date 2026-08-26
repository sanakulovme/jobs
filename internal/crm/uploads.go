package crm

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"faangjobs/internal/store"
)

// MaxUploadBytes bounds a single document upload (CV/cover letter/motivation
// letter are all small documents; this is a generous safety cap, not a
// realistic expected size).
const MaxUploadBytes = 20 << 20 // 20 MiB

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitizeFilename strips anything that isn't safe in a path segment,
// mirroring internal/store's safeName convention for company ids.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	clean := unsafeFilenameChars.ReplaceAllString(name, "_")
	clean = strings.Trim(clean, "._")
	if clean == "" {
		clean = "file"
	}
	return clean
}

// SaveUpload reads r fully (bounded by MaxUploadBytes by the caller, via
// http.MaxBytesReader) and writes it atomically under
// UploadsDir()/candidateID/docID_filename, returning the path stored on the
// Document record (relative to the CRM data dir) and the byte size written.
func (s *Store) SaveUpload(candidateID, docID, originalFilename string, r io.Reader) (storedPath string, size int64, err error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("read upload: %w", err)
	}
	if len(data) > MaxUploadBytes {
		return "", 0, fmt.Errorf("upload exceeds %d byte limit", MaxUploadBytes)
	}

	dir := filepath.Join(s.uploadsDir, sanitizeFilename(candidateID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create upload dir: %w", err)
	}

	filename := docID + "_" + sanitizeFilename(originalFilename)
	full := filepath.Join(dir, filename)
	if err := store.WriteFileAtomic(full, data); err != nil {
		return "", 0, fmt.Errorf("write upload: %w", err)
	}

	rel, err := filepath.Rel(s.dir, full)
	if err != nil {
		rel = full
	}
	return rel, int64(len(data)), nil
}

// OpenUpload opens a previously-saved document file for reading (streaming
// it back to the client, e.g. via http.ServeContent).
func (s *Store) OpenUpload(storedPath string) (*os.File, error) {
	full := filepath.Join(s.dir, storedPath)
	// Reject any path that escapes the CRM data dir (a defensively-checked
	// invariant — storedPath is always produced by SaveUpload, never user
	// input, but this keeps the guarantee explicit).
	rel, err := filepath.Rel(s.dir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("invalid stored path %q", storedPath)
	}
	return os.Open(full)
}
