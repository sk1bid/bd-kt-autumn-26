package kt03

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"transport-monitoring/internal/kt02"
)

type runner struct {
	ctx                               context.Context
	root, db, out, mode, data, method string
	seed                              int64
	rows, runs                        int
}

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: kt03 pilot|generate|load|verify|queries|bench|scenarios|load-compare|environment|regression|report|package [flags]")
	}
	r := &runner{ctx: ctx}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.StringVar(&r.root, "root", ".", "project root")
	f.StringVar(&r.db, "database", "transport_kt03", "isolated database")
	f.StringVar(&r.out, "out", "artifacts/kt03", "results directory")
	f.StringVar(&r.data, "data", "data/kt03", "generated data directory")
	f.StringVar(&r.mode, "mode", "dev", "dev or load")
	f.StringVar(&r.method, "method", "copy", "copy or insert")
	f.Int64Var(&r.seed, "seed", 20261011, "deterministic seed")
	f.IntVar(&r.rows, "rows", 0, "message count override for experiments")
	f.IntVar(&r.runs, "runs", 7, "measured runs")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if r.mode != "dev" && r.mode != "load" {
		return errors.New("mode must be dev or load")
	}
	if r.method != "copy" && r.method != "insert" {
		return errors.New("method must be copy or insert")
	}
	if r.rows < 0 || r.runs < 1 {
		return errors.New("invalid rows or runs")
	}
	var err error
	r.root, err = filepath.Abs(r.root)
	if err != nil {
		return err
	}
	r.out = resolve(r.root, r.out)
	r.data = resolve(r.root, r.data)
	if err = os.MkdirAll(r.out, 0755); err != nil {
		return err
	}
	switch args[0] {
	case "pilot":
		return r.pilot()
	case "generate":
		return r.generate()
	case "load":
		return r.load()
	case "verify":
		return r.verify()
	case "queries":
		return r.queries()
	case "bench":
		return r.bench()
	case "scenarios":
		return r.scenarios()
	case "load-compare":
		return r.loadCompare()
	case "package":
		return r.archive()
	case "report":
		return r.report()
	case "environment":
		return r.environment()
	case "regression":
		return r.regression()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func resolve(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}
func lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func (r *runner) command(db string) *exec.Cmd {
	c := exec.CommandContext(r.ctx, "docker", "compose", "--profile", "test", "exec", "-T", "db", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-v", "VERBOSITY=verbose", "-U", "kt02", "-d", db)
	c.Dir = r.root
	return c
}
func (r *runner) sql(db, sql string) (string, error) {
	return r.input(db, strings.NewReader("SET search_path=transport,public; SET TIME ZONE 'Asia/Novosibirsk'; SET statement_timeout='10min'; SET lock_timeout='20s';\n"+sql))
}
func (r *runner) input(db string, in io.Reader) (string, error) {
	c := r.command(db)
	c.Stdin = in
	var out, stderr bytes.Buffer
	c.Stdout = &out
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return out.String(), fmt.Errorf("%w: %s", err, stderr.String())
	}
	return out.String(), nil
}
func (r *runner) file(db, path string) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		return "", err
	}
	return r.sql(db, string(b))
}
func (r *runner) reset(db string, target int) error {
	if !strings.HasPrefix(db, "transport_kt03") {
		return errors.New("reset requires transport_kt03 prefix")
	}
	for _, c := range db {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return errors.New("invalid database name")
		}
	}
	if _, err := r.sql("postgres", "DROP DATABASE IF EXISTS "+db+";"); err != nil {
		return err
	}
	if _, err := r.sql("postgres", "CREATE DATABASE "+db+" TEMPLATE template0;"); err != nil {
		return err
	}
	return kt02.Run(r.ctx, []string{"migrate", "--database", db, "--target", fmt.Sprint(target), "--root", r.root}, io.Discard, os.Stderr)
}
func (r *runner) save(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return r.saveText(name, string(b)+"\n")
}
func (r *runner) saveText(name, s string) error {
	p := filepath.Join(r.out, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(s), 0644)
}
func (r *runner) invalidate(names ...string) error {
	for _, name := range names {
		if err := os.Remove(filepath.Join(r.out, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func ms(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000 }

func (r *runner) pilot() error {
	type result struct {
		Version int     `json:"version"`
		Rows    int     `json:"rows"`
		WallMS  float64 `json:"wall_ms"`
		Output  string  `json:"server_timings"`
	}
	var results []result
	db := "transport_kt03_pilot"
	defer r.sql("postgres", "DROP DATABASE IF EXISTS "+db+";")
	for _, version := range []int{4, 5} {
		for _, n := range []int{200, 1000, 2000} {
			if err := r.reset(db, version); err != nil {
				return err
			}
			if _, err := r.file(db, "sql/seed.sql"); err != nil {
				return err
			}
			q := fmt.Sprintf(`SET statement_timeout='60s';
\timing on
BEGIN;
INSERT INTO message_key(device_id,message_id,content_sha256,first_received_at,generated_at,installation_id,first_receipt_id,result)
SELECT 'demo-device-1',md5('message-'||g)::uuid,decode(repeat('ab',32),'hex'),'2026-09-22 10:00+07'::timestamptz+g*interval '1 second','2026-09-22 10:00+07'::timestamptz+g*interval '1 second',1,md5('receipt-'||g)::uuid,'accepted' FROM generate_series(1,%d) g;
COMMIT;
`, n)
			start := time.Now()
			out, err := r.sql(db, q)
			if err != nil {
				return err
			}
			results = append(results, result{version, n, ms(start), out})
			fmt.Printf("pilot migration=%d rows=%d wall=%.1f ms\n", version, n, results[len(results)-1].WallMS)
			if err = r.save("pilot.json", results); err != nil {
				return err
			}
		}
	}
	return nil
}
