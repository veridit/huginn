package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// renameDestinationAvailable permits a case-only rename on a case-insensitive
// filesystem, but never permits overwriting a distinct directory entry. SameFile
// alone is insufficient: two differently named entries could be hard links.
func renameDestinationAvailable(source, dest string) (bool, error) {
	st, err := os.Lstat(dest)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(filepath.Clean(source), filepath.Clean(dest)) {
		return false, nil
	}
	original, err := os.Lstat(source)
	if err != nil {
		return false, err
	}
	if !os.SameFile(original, st) {
		return false, nil
	}
	// On a case-sensitive filesystem even case-equivalent hard links are
	// separate entries. Inspect the directory's actual spelling to reject them.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(dest) {
			return false, nil
		}
	}
	return true, nil
}

// renameFile uses an intermediate directory entry for case-only changes, so
// both case-insensitive filesystems and git observe the final spelling.
func renameFile(source, dest string) error {
	if source == dest || !strings.EqualFold(source, dest) {
		return os.Rename(source, dest)
	}
	tmp, err := os.CreateTemp(filepath.Dir(source), ".huginn-rename-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err = tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err = os.Rename(source, name); err != nil {
		os.Remove(name)
		return err
	}
	if err = os.Rename(name, dest); err != nil {
		return errors.Join(err, os.Rename(name, source))
	}
	return nil
}

// diskPath rejects traversal and symlinked paths before local mutations.
func (s *Store) diskPath(rel string) (string, error) {
	root, e := filepath.Abs(s.Root)
	if e != nil {
		return "", e
	}
	if filepath.IsAbs(rel) {
		return "", &Error{Kind: "unsafe-path", Value: rel, Fix: "huginn lint"}
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", &Error{Kind: "unsafe-path", Value: rel, Fix: "huginn lint"}
	}
	p := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", &Error{Kind: "unsafe-symlink", Value: rel, Fix: "huginn lint"}
		}
	}
	return filepath.Join(root, clean), nil
}
