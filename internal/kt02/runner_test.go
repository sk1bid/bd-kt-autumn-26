package kt02

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown", "--root", t.TempDir()}, {"migrate", "--target", "0"}, {"test", "unexpected"}, {"test", "--unknown"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), args, &output, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestMigrationPlanKeepsOrderTargetAndChecksum(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "migrations")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"002_second.sql": "SELECT 2;\n", "001_first.sql": "SELECT 1;\n", "003_last.sql": "SELECT 3;\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	script, err := migrationScript(root, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, "SELECT 3;") || strings.Index(script, "SELECT 1;") > strings.Index(script, "SELECT 2;") {
		t.Fatal("target or ordering lost")
	}
	if !strings.HasPrefix(script, "BEGIN;") || !strings.HasSuffix(script, "COMMIT;\n") || !strings.Contains(script, "pg_advisory_xact_lock") || !strings.Contains(script, `\if :pending`) {
		t.Fatal("migration transaction/skip contract lost")
	}
	changed, err := migrationScript(root, 2, func(v int, c []byte) []byte {
		if v == 1 {
			return append(c, []byte("SELECT 10;\n")...)
		}
		return c
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed == script || !strings.Contains(changed, "checksum mismatch") {
		t.Fatal("modified SQL cannot be detected")
	}
	if !strings.Contains(script, "unknown to this release") {
		t.Fatal("unknown applied migrations are not rejected")
	}

}

func TestDictionaryRejectsUndocumentedSchema(t *testing.T) {
	if _, _, err := dictionaryMarkdown(catalog{Tables: []table{{Name: "unknown"}}}); err == nil {
		t.Fatal("undocumented table accepted")
	}
	if _, _, err := dictionaryMarkdown(catalog{Tables: []table{{Name: "vehicle", Columns: []column{{Name: "unknown"}}}}}); err == nil {
		t.Fatal("undocumented column accepted")
	}
}

func TestChecksumIgnoresCommentsAndWhitespaceOnly(t *testing.T) {
	base := []byte("CREATE TABLE a (id int);\nSELECT 1;\n")
	edited := []byte("-- note\r\nCREATE TABLE a (id int);   \n\n  -- more\nSELECT 1;")
	if digest(normalizeSQL(base)) != digest(normalizeSQL(edited)) {
		t.Fatal("comment-only edit changes checksum")
	}
	if digest(normalizeSQL(base)) == digest(normalizeSQL([]byte("CREATE TABLE a (id bigint);\nSELECT 1;\n"))) {
		t.Fatal("SQL edit keeps checksum")
	}
}
