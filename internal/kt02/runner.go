package kt02

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed assets/*
var assets embed.FS

type runner struct {
	ctx      context.Context
	root     string
	local    bool
	database string
	out      io.Writer
}

type validation struct {
	Status    string   `json:"status"`
	Server    string   `json:"server"`
	CheckedAt string   `json:"checked_at_utc"`
	Checks    []string `json:"checks"`
}

func Run(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: kt02 <migrate|seed|demo|test|catalog|dictionary> [--local] [--database transport] [--target 3] [--root path]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	local := flags.Bool("local", false, "use local PostgreSQL CLI and PG* environment")
	database := flags.String("database", "transport", "database name")
	target := flags.Int("target", 999, "last migration to apply")
	root := flags.String("root", "", "project root (default: find from working directory)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *target < 1 {
		return errors.New("target must be positive")
	}
	var err error
	if *root == "" {
		*root, err = findRoot()
		if err != nil {
			return err
		}
	} else {
		*root, err = filepath.Abs(*root)
		if err != nil {
			return err
		}
	}
	r := &runner{ctx: ctx, root: *root, local: *local, database: *database, out: out}
	switch args[0] {
	case "migrate":
		if err := r.migrate(*database, *target, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, "Migrations applied")
	case "seed", "demo":
		name := "sql/seed.sql"
		if args[0] == "demo" {
			name = "sql/lifecycles.sql"
		}
		result, err := r.fileSQL(*database, name)
		if err != nil {
			return err
		}
		fmt.Fprint(out, result)
	case "test":
		return r.test()
	case "catalog":
		result, err := r.sql(*database, "SELECT table_name,column_name,data_type,is_nullable FROM information_schema.columns WHERE table_schema='transport' ORDER BY table_name,ordinal_position;")
		if err != nil {
			return err
		}
		fmt.Fprint(out, result)
	case "dictionary":
		return r.dictionary()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "migrations/001_schema.sql")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("project root not found; use --root")
		}
		dir = parent
	}
}
func (r *runner) command(ctx context.Context, program string, args ...string) *exec.Cmd {
	if !r.local {
		args = append([]string{"compose", "--profile", "test", "exec", "-T", "db", program, "-U", "kt02"}, args...)
		program = "docker"
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = r.root
	return cmd
}
func (r *runner) sql(database, statement string) (string, error) {
	return r.sqlContext(r.ctx, database, statement)
}
func (r *runner) sqlContext(ctx context.Context, database, statement string) (string, error) {
	cmd := r.command(ctx, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-At", "-d", database)
	cmd.Stdin = strings.NewReader(statement)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("psql: %w\n%s%s", err, stderr.String(), stdout.String())
	}
	return stdout.String(), nil
}
func (r *runner) fileSQL(database, path string) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		return "", err
	}
	return r.sql(database, string(b))
}
func literal(s string) string    { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func identifier(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

type mutation func(version int, content []byte) []byte

func normalizeSQL(content []byte) []byte {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		lines = append(lines, line)
	}
	return []byte(strings.Join(lines, "\n"))
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func legacyChecksums() (map[string][]string, error) {
	raw, err := assets.ReadFile("assets/legacy-checksums.json")
	if err != nil {
		return nil, err
	}
	var sums map[string][]string
	return sums, json.Unmarshal(raw, &sums)
}

func migrationScript(root string, target int, mutate mutation) (string, error) {
	files, err := filepath.Glob(filepath.Join(root, "migrations/[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("no migrations found")
	}
	sort.Strings(files)
	legacy, err := legacyChecksums()
	if err != nil {
		return "", err
	}
	latest, err := strconv.Atoi(filepath.Base(files[len(files)-1])[:3])
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("BEGIN; SELECT pg_advisory_xact_lock(20260922,2);\nCREATE TABLE IF NOT EXISTS public.schema_migrations (version integer PRIMARY KEY, name text NOT NULL, sha256 text NOT NULL, applied_at timestamptz NOT NULL DEFAULT current_timestamp);\n")
	fmt.Fprintf(&b, "DO $$ BEGIN IF EXISTS (SELECT 1 FROM public.schema_migrations WHERE version>%d) THEN RAISE EXCEPTION USING MESSAGE=%s; END IF; END $$;\n", latest, literal("Database contains migrations unknown to this release"))
	for _, path := range files {
		name := filepath.Base(path)
		version, err := strconv.Atoi(name[:3])
		if err != nil {
			return "", err
		}
		if version > target {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if mutate != nil {
			content = mutate(version, content)
		}
		current := digest(normalizeSQL(content))
		accepted := []string{literal(current), literal(digest(content))}
		for _, sum := range legacy[strconv.Itoa(version)] {
			accepted = append(accepted, literal(sum))
		}
		fmt.Fprintf(&b, "DO $$ BEGIN IF EXISTS (SELECT 1 FROM public.schema_migrations WHERE version=%d AND (sha256 NOT IN (%s) OR name<>%s)) THEN RAISE EXCEPTION USING MESSAGE=%s; END IF; END $$;\n", version, strings.Join(accepted, ","), literal(name), literal("Migration checksum mismatch: "+name))
		fmt.Fprintf(&b, "SELECT NOT EXISTS(SELECT 1 FROM public.schema_migrations WHERE version=%d) AS pending \\gset\n\\if :pending\n", version)
		b.Write(content)
		b.WriteByte('\n')
		fmt.Fprintf(&b, "INSERT INTO public.schema_migrations(version,name,sha256) VALUES(%d,%s,%s);\n\\endif\n", version, literal(name), literal(current))
	}
	b.WriteString("COMMIT;\n")
	return b.String(), nil
}
func (r *runner) migrate(database string, target int, mutate mutation) error {
	script, err := migrationScript(r.root, target, mutate)
	if err != nil {
		return err
	}
	_, err = r.sql(database, script)
	return err
}
func (r *runner) signature(database string) (string, error) {
	b, err := assets.ReadFile("assets/signature.sql")
	if err != nil {
		return "", err
	}
	return r.sql(database, string(b))
}
func (r *runner) tables(database string) ([]string, error) {
	tables, err := r.sql(database, "SELECT tablename FROM pg_tables WHERE schemaname='transport' ORDER BY tablename;")
	return strings.Fields(tables), err
}
func (r *runner) snapshot(database string, tables []string) (string, error) {
	var rows []string
	for _, table := range tables {
		s, err := r.sql(database, "SELECT to_jsonb(t)::text FROM transport."+identifier(table)+" t ORDER BY to_jsonb(t)::text;")
		if err != nil {
			return "", err
		}
		rows = append(rows, table+s)
	}
	return strings.Join(rows, "\n"), nil
}
func same(a, b, label string) error {
	if a != b {
		return fmt.Errorf("%s: values differ", label)
	}
	return nil
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'))
}
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".kt02-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(b); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(temp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
