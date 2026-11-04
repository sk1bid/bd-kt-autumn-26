package kt04

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubmissionExcludesWorkingMaterials(t *testing.T) {
	selected := map[string]bool{}
	for _, path := range submissionFiles() {
		if selected[path] {
			t.Fatalf("duplicate file: %s", path)
		}
		selected[path] = true
		if filepath.IsAbs(path) || strings.Contains(path, "..") {
			t.Fatalf("unsafe path: %s", path)
		}
	}
	for _, path := range []string{"CLAUDE.md", "AGENTS.md", "planning/kt04-handoff.md", "docs/kt04/live-guide.md", ".env", "secrets/postgres.password", "submission/KT-04.zip", "data/kt03-load/message_key.csv", "web/package.json"} {
		if selected[path] {
			t.Errorf("working file included: %s", path)
		}
	}
	for _, path := range []string{"docs/kt04/README.md", "docs/kt04/runbook.md", "sql/kt04/security.sql", "deploy/kt04/compose.yaml", "artifacts/kt04/pitr.json", "cmd/kt04-client/main.go"} {
		if !selected[path] {
			t.Errorf("required file missing: %s", path)
		}
	}
}

func TestPackageRequiresAllEvidence(t *testing.T) {
	root := t.TempDir()
	r := New(root)
	dir := filepath.Join(root, "artifacts/kt04")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"environment", "security", "restore", "replication", "analytics", "failures", "lag"} {
		if e := os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Package(); e == nil || !strings.Contains(e.Error(), "pitr.json") {
		t.Fatalf("expected missing PITR evidence, got %v", e)
	}
	if _, e := os.Stat(filepath.Join(root, "submission/KT-04.zip")); !os.IsNotExist(e) {
		t.Fatal("incomplete package created")
	}
}
