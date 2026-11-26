package agentapi

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// An ExtractedFile is one file of a download and what Extract did with it.
type ExtractedFile struct {
	// Name is the file's path in the Agent session directory, with slashes.
	Name string
	// Flags is "" when Extract wrote the file. Else it is the file's
	// PAXFlags ("partial" or "cut"), and Extract did not write the file.
	Flags string
}

// Extract writes each file of a download's tar archive into root. It
// gives what it did with each file, in the sequence of the archive.
//
// Extract calls give with the path of each folder and file that it makes,
// for example to change the owner. give can be nil. Extract does not call
// give for a partial or cut file.
//
// Extract does not write a partial file (Retention dropped its start) or a
// cut file (it has missing ranges). The offsets of such a file do not
// agree with the offsets in SolidTrace, and a resume from it is not
// promised. A path that is not a plain relative path makes Extract return
// an error.
func Extract(archive io.Reader, root *os.Root, give func(name string) error) ([]ExtractedFile, error) {
	if give == nil {
		give = func(string) error { return nil }
	}
	var files []ExtractedFile
	r := tar.NewReader(archive)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read the download: %w", err)
		}
		// SolidTrace stores files only: no folder, link or device.
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name := h.Name
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || name == ".." ||
			strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("the download has the path %q, which is not in the Agent session directory", name)
		}
		if flags := h.PAXRecords[PAXFlags]; flags != "" {
			files = append(files, ExtractedFile{Name: name, Flags: flags})
			continue
		}
		// Each folder of the path that is not there yet.
		parts := strings.Split(name, "/")
		for i := 1; i < len(parts); i++ {
			folder := filepath.Join(parts[:i]...)
			if err := root.Mkdir(folder, 0o700); err == nil {
				if err := give(folder); err != nil {
					return nil, err
				}
			} else if !errors.Is(err, os.ErrExist) {
				return nil, fmt.Errorf("extract %s: %w", name, err)
			}
		}
		file := filepath.FromSlash(name)
		f, err := root.OpenFile(file, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}
		_, err = io.Copy(f, r)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = give(file)
		}
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}
		files = append(files, ExtractedFile{Name: name})
	}
}
