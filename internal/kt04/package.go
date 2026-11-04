package kt04

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Package writes the KT-04 submission after all experiment reports are present.
func (r *Runner) Package() error {
	paths := submissionFiles()
	const evidence = "artifacts/kt04"
	for _, stage := range []string{"environment", "security", "restore", "replication", "analytics", "failures", "lag", "pitr"} {
		path := filepath.Join(evidence, stage+".json")
		data, err := os.ReadFile(filepath.Join(r.Root, path))
		if err != nil {
			return fmt.Errorf("required report %s: %w", path, err)
		}
		if !json.Valid(data) {
			return fmt.Errorf("invalid JSON report: %s", path)
		}
	}

	sort.Strings(paths)

	output := filepath.Join(r.Root, "submission")
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(output, ".kt04-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	archive := zip.NewWriter(file)
	for _, path := range paths {
		if err := addPackageFile(archive, r.Root, path); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(output, "KT-04.zip"))
}

func addPackageFile(archive *zip.Writer, root, path string) error {
	source := filepath.Join(root, path)
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("archive input must be a nonempty regular file: %s", path)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	entry, err := archive.Create(filepath.ToSlash(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, input)
	return err
}

// Explicit allowlist: training guides, AI notes and secrets never enter the submission.
func submissionFiles() []string {
	return []string{
		"artifacts/kt04/analytics.json",
		"artifacts/kt04/environment.json",
		"artifacts/kt04/failures.json",
		"artifacts/kt04/lag.json",
		"artifacts/kt04/pitr.json",
		"artifacts/kt04/replication.json",
		"artifacts/kt04/restore.json",
		"artifacts/kt04/security.json",
		"cmd/kt02/main.go",
		"cmd/kt04-client/README.md",
		"cmd/kt04-client/main.go",
		"cmd/kt04/main.go",
		"deploy/kt04/archive-wal.sh",
		"deploy/kt04/compose.yaml",
		"deploy/kt04/entrypoint.sh",
		"deploy/kt04/pg_hba.conf",
		"deploy/kt04/postgresql.conf",
		"docs/kt04/README.md",
		"docs/kt04/runbook.md",
		"go.mod",
		"internal/kt02/assets/catalog.sql",
		"internal/kt02/assets/descriptions.json",
		"internal/kt02/assets/legacy-checksums.json",
		"internal/kt02/assets/signature.sql",
		"internal/kt02/catalog.go",
		"internal/kt02/runner.go",
		"internal/kt02/runner_test.go",
		"internal/kt02/verify.go",
		"internal/kt04/analytics.go",
		"internal/kt04/package.go",
		"internal/kt04/recovery.go",
		"internal/kt04/runner.go",
		"internal/kt04/security.go",
		"internal/kt04/setup.go",
		"internal/kt04/setup_test.go",
		"migrations/001_schema.sql",
		"migrations/002_operations.sql",
		"migrations/003_reporting.sql",
		"migrations/004_integrity.sql",
		"migrations/005_batch_validation.sql",
		"sql/kt04/security.sql",
		"sql/seed.sql",
		"tests/kt04/security.sql",
		"internal/kt04/package_test.go",
	}
}
