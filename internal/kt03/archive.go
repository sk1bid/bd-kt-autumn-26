package kt03

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func submissionPath(path string) bool {
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "artifacts/kt03/") {
		name := strings.TrimPrefix(path, "artifacts/kt03/")
		switch name {
		case "README.md", "environment.json", "pilot.json", "regression.json", "business-regression.json", "load-comparison.json",
			"manifest-dev.json", "manifest-load.json", "dev/verification-dev.json", "load/load-load.json", "load/verification-load.json",
			"scenarios/commit.json", "scenarios/rollback.json", "scenarios/rollback.txt", "scenarios/concurrency.json", "scenarios/lock-wait.json", "scenarios/session-second.txt":
			return true
		}
		return strings.HasPrefix(name, "load/queries/") || strings.HasPrefix(name, "load/bench/") || strings.HasPrefix(name, "load/plans/")
	}
	if strings.HasPrefix(path, "docs/kt03/") {
		return path == "docs/kt03/README.md" || path == "docs/kt03/01-report.md" || path == "docs/kt03/report.tmpl"
	}
	if path == "internal/kt03/tasks.mk" {
		return true
	}
	for _, prefix := range []string{"cmd/", "internal/", "migrations/", "sql/", "tests/"} {
		if strings.HasPrefix(path, prefix) {
			switch filepath.Ext(path) {
			case ".go", ".sql", ".json":
				return true
			}
			return false
		}
	}
	return path == "go.mod" || path == "compose.yaml" || path == "Makefile"
}

func (r *runner) archive() error {
	if err := r.checkPackage(); err != nil {
		return err
	}
	var paths []string
	for _, name := range []string{"go.mod", "compose.yaml", "Makefile", "cmd", "internal", "migrations", "sql", "tests", "docs/kt03", "artifacts/kt03"} {
		err := filepath.Walk(filepath.Join(r.root, name), func(p string, i os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			if i.Mode().IsRegular() {
				rel, err := filepath.Rel(r.root, p)
				if err != nil {
					return err
				}
				if submissionPath(rel) {
					paths = append(paths, p)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(paths)
	path := filepath.Join(r.root, "submission/KT-03.zip")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".kt03-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	z := zip.NewWriter(f)
	for _, p := range paths {
		rel, err := filepath.Rel(r.root, p)
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		w, err := z.Create(filepath.ToSlash(rel))
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		if rel == "Makefile" {
			if _, err = io.WriteString(w, "include internal/kt03/tasks.mk\n"); err != nil {
				z.Close()
				f.Close()
				return err
			}
			continue
		}
		source, err := os.Open(p)
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		_, err = io.Copy(w, source)
		source.Close()
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
	}
	if err = z.Close(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(f.Name(), 0644); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	fmt.Printf("Archive: %s (%d files)\n", path, len(paths))
	return nil
}
