package kt03

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func fileDigests(root string, paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range paths {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out[path] = hex.EncodeToString(sum[:])
	}
	return out, nil
}
func queryPaths() []string {
	var paths []string
	for _, name := range queryFiles {
		paths = append(paths, "sql/kt03/queries/"+name+".sql")
	}
	return paths
}
