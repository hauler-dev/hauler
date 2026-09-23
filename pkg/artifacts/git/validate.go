package git

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ValidateName rejects repository names that would be served or extracted onto or outside their root directory, e.g. "." or "..".
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || strings.Contains(name, `\`) {
		return fmt.Errorf("invalid git repository name [%s]", name)
	}
	return nil
}

// ValidateBareRepo reports whether dir looks like a bare git repository
// missing a HEAD file, an objects directory, or a refs directory or a packed-refs file
func ValidateBareRepo(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return fmt.Errorf("unable to verify [%s] as a git repository... missing HEAD file: %w", dir, err)
	}

	if info, err := os.Stat(filepath.Join(dir, "objects")); err != nil || !info.IsDir() {
		return fmt.Errorf("unable to verify [%s] as a git repository... missing objects directory", dir)
	}

	_, refsErr := os.Stat(filepath.Join(dir, "refs"))
	_, packedErr := os.Stat(filepath.Join(dir, "packed-refs"))
	if refsErr != nil && packedErr != nil {
		return fmt.Errorf("unable to verify [%s] as a git repository... missing refs directory or packed-refs file", dir)
	}

	return nil
}
