package task

import (
	"os"
	"path/filepath"
	"strings"
)

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
