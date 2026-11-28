package service

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"findus/backend/internal/domain"
)

// DataFile resolves a stored relative path such as photo_path and refuses everything outside
// dataDir/subdir, where the database or the JWT secret would be reachable.
func DataFile(dataDir, subdir, rel string) (string, error) {
	clean := path.Clean(rel)
	if !strings.HasPrefix(clean, subdir+"/") || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("file path %q: %w", rel, domain.ErrNotFound)
	}
	return filepath.Join(dataDir, filepath.FromSlash(clean)), nil
}
