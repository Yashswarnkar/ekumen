package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// mkdirs creates each directory from the root down to dir (dir must be
// inside the root), giving any directory it creates mode DirMode
// regardless of the umask.
func (s *Store) mkdirs(dir string) error {
	rel, err := filepath.Rel(s.root, dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.root); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(s.root, DirMode); err != nil {
			return err
		}
		if err := os.Chmod(s.root, DirMode); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	cur := s.root
	if rel == "." {
		return nil
	}
	for _, part := range splitPath(rel) {
		cur = filepath.Join(cur, part)
		err := os.Mkdir(cur, DirMode)
		switch {
		case err == nil:
			if err := os.Chmod(cur, DirMode); err != nil {
				return err
			}
		case errors.Is(err, fs.ErrExist):
			fi, serr := os.Stat(cur)
			if serr != nil {
				return serr
			}
			if !fi.IsDir() {
				return fmt.Errorf("files: %s exists and is not a directory", cur)
			}
		default:
			return err
		}
	}
	return nil
}

func splitPath(rel string) []string {
	return strings.Split(filepath.ToSlash(rel), "/")
}

// writeNew writes data to dir/name atomically and never replaces an
// existing file. The data goes to a temp file in the same directory, which
// is synced and then committed with a hard link (which fails if the target
// exists). If the filesystem cannot hard-link, it falls back to a checked
// rename, serialized within this process.
//
// If the target exists the returned error wraps fs.ErrExist. On any error
// the temp file is removed and no file appears at the target.
func (s *Store) writeNew(dir, name string, data []byte) (string, error) {
	final := filepath.Join(dir, name)
	if err := s.mkdirs(dir); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	tmpGone := false
	defer func() {
		if !tmpGone {
			os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Chmod(FileMode); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if s.beforeCommit != nil {
		if err := s.beforeCommit(tmp); err != nil {
			return "", err
		}
	}

	link := os.Link
	if s.link != nil {
		link = s.link
	}
	err = link(tmp, final)
	switch {
	case err == nil:
		// tmp is removed by the deferred call; final is the committed file.
	case errors.Is(err, fs.ErrExist):
		return "", existErr(final)
	default:
		// No hard links here: check, then rename. Rename would replace an
		// existing file, so the check and rename are serialized within
		// this process. Writers in other processes use different names
		// (ULIDs, or timestamp plus author), so a race needs two writers
		// creating the same name in the same instant.
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, serr := os.Lstat(final); serr == nil {
			return "", existErr(final)
		} else if !errors.Is(serr, fs.ErrNotExist) {
			return "", serr
		}
		if rerr := os.Rename(tmp, final); rerr != nil {
			return "", rerr
		}
		tmpGone = true
	}
	syncDir(dir)
	return final, nil
}

func existErr(path string) error {
	return fmt.Errorf("files: %s already exists; the store never overwrites: %w", path, fs.ErrExist)
}

// syncDir flushes a directory entry to disk. Best effort: not every
// platform supports it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile returns the lowercase hex SHA-256 of the file's bytes, the form
// recorded as text_sha256 in events (C-event-log §6, §7.3).
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
