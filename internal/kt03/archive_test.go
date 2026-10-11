package kt03

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmissionExcludesWorkingMaterials(t *testing.T) {
	for _, path := range []string{"CLAUDE.md", "README.md", ".env", "data/kt03-load/message_key.csv", "artifacts/kt03/full-run.txt", "artifacts/kt03/baseline/environment.json", "artifacts/kt03/dev/plans/schedule-after-01.json", "artifacts/kt03/dev/queries/01_schedule.json", "artifacts/kt03/scenarios/commit.txt", "docs/kt03/draft.md", "internal/kt03/debug.log"} {
		if submissionPath(path) {
			t.Errorf("working material included: %s", path)
		}
	}
	for _, path := range []string{"cmd/kt03/main.go", "docs/kt03/01-report.md", "docs/kt03/report.tmpl", "artifacts/kt03/load/plans/schedule-after-01.json", "artifacts/kt03/load/bench/measurements.json", "artifacts/kt03/dev/verification-dev.json", "artifacts/kt03/scenarios/lock-wait.json"} {
		if !submissionPath(path) {
			t.Errorf("submission artifact excluded: %s", path)
		}
	}
}

func TestArchiveRejectsMissingRequiredQueryAndPlan(t *testing.T) {
	for _, missing := range []string{"sql/kt03/queries/04_message_quality.sql", "artifacts/kt03/load/plans/schedule-after-01.json"} {
		t.Run(filepath.Base(missing), func(t *testing.T) {
			root := t.TempDir()
			for _, name := range requiredPackageFiles() {
				if name == missing {
					continue
				}
				p := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(`{"status":"PASS"}`), 0644); err != nil {
					t.Fatal(err)
				}
			}
			r := &runner{ctx: context.Background(), root: root}
			err := r.archive()
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("expected missing %s, got %v", missing, err)
			}
			if _, err = os.Stat(filepath.Join(root, "submission/KT-03.zip")); !os.IsNotExist(err) {
				t.Fatal("incomplete archive created")
			}
		})
	}
}

func TestFailedRerunInvalidatesOldSuccess(t *testing.T) {
	for _, command := range []string{"load", "verify", "queries", "bench", "load-compare"} {
		t.Run(command, func(t *testing.T) {
			out := t.TempDir()
			paths := map[string]string{"load": "verification-dev.json", "verify": "verification-dev.json", "queries": "queries/verification.json", "bench": "bench/verification.json", "load-compare": "load-comparison.json"}
			p := filepath.Join(out, paths[command])
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(`{"status":"PASS"}`), 0644); err != nil {
				t.Fatal(err)
			}
			err := Run(context.Background(), []string{command, "--root", t.TempDir(), "--out", out, "--data", "missing"})
			if err == nil {
				t.Fatal("expected missing input failure")
			}
			if _, err = os.Stat(p); !os.IsNotExist(err) {
				t.Fatal("old PASS survived failed run")
			}
		})
	}
}
