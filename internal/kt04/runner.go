package kt04

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const DB = "transport_kt04"

type Result struct {
	Out  string `json:"output"`
	Err  string `json:"error,omitempty"`
	Code int    `json:"exit_code"`
}
type Runner struct{ Root, Secrets, BackupDir string }

func New(root string) *Runner {
	r := &Runner{Root: root, Secrets: os.Getenv("KT04_SECRETS_DIR"), BackupDir: os.Getenv("KT04_BACKUP_DIR")}
	if r.Secrets == "" {
		r.Secrets = "/private/tmp/transport-kt04-secrets"
	}
	if r.BackupDir == "" {
		r.BackupDir = "/private/tmp/transport-kt04-backups"
	}
	return r
}
func (r *Runner) Command(input string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = r.Root
	cmd.Stdin = strings.NewReader(input)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Out: out.String(), Err: stderr.String()}
	if err != nil {
		res.Code = -1
		if cmd.ProcessState != nil {
			res.Code = cmd.ProcessState.ExitCode()
		}
		return res, fmt.Errorf("%s failed: %s", args[0], res.Err)
	}
	return res, nil
}
func (r *Runner) compose(args ...string) []string {
	return append([]string{"docker", "compose", "-f", filepath.Join(r.Root, "deploy/kt04/compose.yaml"), "-p", "transport-kt04"}, args...)
}
func (r *Runner) DC(args ...string) (Result, error) { return r.Command("", r.compose(args...)...) }
func (r *Runner) SQL(q, service, role, database, ssl string) (Result, error) {
	if service == "" {
		service = "primary"
	}
	if role == "" {
		role = "postgres"
	}
	if database == "" {
		database = DB
	}
	if ssl == "" {
		ssl = "verify-full"
	}
	return r.Command(q, r.compose("exec", "-T", "-e", "PGSSLMODE="+ssl, "-e", "PGSSLROOTCERT=/run/secrets/ca.crt", "-e", "PGPASSFILE=/run/secrets/"+role+".pgpass", "-e", "PGCONNECT_TIMEOUT=3", service, "psql", "-X", "-w", "-h", service, "-U", role, "-d", database, "-At", "-v", "ON_ERROR_STOP=1", "-v", "VERBOSITY=verbose")...)
}
func (r *Runner) Client(service, role, mode string, extra ...string) (Result, error) {
	args := []string{"exec", "-T", "-e", "PGHOST=" + service, "-e", "PGUSER=" + role, "-e", "PGDATABASE=" + DB, "-e", "PGPASSFILE=/run/secrets/" + role + ".pgpass", "-e", "PGSSLROOTCERT=/run/secrets/ca.crt", "client", "/work/bin/kt04-client", mode}
	return r.DC(append(args, extra...)...)
}
func (r *Runner) Save(name string, value any) error {
	dir := filepath.Join(r.Root, "artifacts/kt04")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(dir, name+".json"), append(b, '\n'), 0644)
	if err == nil {
		fmt.Println(name + ": PASS")
	}
	return err
}
func Wait(fn func() (bool, error), timeout time.Duration) error {
	end := time.Now().Add(timeout)
	var last error
	for time.Now().Before(end) {
		ok, err := fn()
		if err == nil && ok {
			return nil
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for expected state: %v", last)
}
func (r *Runner) Passwords(service string) error {
	for _, role := range []string{"kt04_admin", "kt04_app", "kt04_analyst", "kt04_driver1", "kt04_driver2", "kt04_replicator"} {
		b, err := os.ReadFile(filepath.Join(r.Secrets, role+".password"))
		if err != nil {
			return err
		}
		// Only stdin carries the password. SQL errors are never echoed for this step.
		_, err = r.SQL("ALTER ROLE "+role+" PASSWORD '"+strings.ReplaceAll(strings.TrimSpace(string(b)), "'", "''")+"';", service, "postgres", "postgres", "")
		if err != nil {
			return fmt.Errorf("password provisioning failed for %s", role)
		}
	}
	return nil
}
func (r *Runner) Init() error {
	if _, err := r.DC("up", "-d", "--wait", "primary", "client"); err != nil {
		return err
	}
	p, err := r.SQL("SELECT 1 FROM pg_database WHERE datname='"+DB+"'", "", "", "postgres", "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.Out) != "" {
		return fmt.Errorf("KT04 database exists; init will not replace it")
	}
	if _, err = r.SQL("CREATE DATABASE "+DB, "", "", "postgres", ""); err != nil {
		return err
	}
	if _, err = r.DC("exec", "-T", "-u", "postgres", "primary", "/work/bin/kt04-migrate", "migrate", "--local", "--database", DB, "--root", "/work"); err != nil {
		return err
	}
	for _, f := range []string{"sql/seed.sql", "sql/kt04/security.sql"} {
		b, e := os.ReadFile(filepath.Join(r.Root, f))
		if e != nil {
			return e
		}
		if _, e = r.SQL(string(b), "", "", "", ""); e != nil {
			return e
		}
	}
	if _, err = r.SQL("CREATE ROLE kt04_replicator LOGIN REPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS", "", "", "postgres", ""); err != nil {
		return err
	}
	if err = r.Passwords("primary"); err != nil {
		return err
	}
	p, err = r.SQL("SELECT version()", "", "", "", "")
	if err != nil {
		return err
	}
	return r.Save("environment", map[string]any{"checked_at": time.Now().UTC(), "server": strings.TrimSpace(p.Out), "database": DB, "project": "transport-kt04", "template": "Own educational configuration; teacher template not supplied"})
}
func (r *Runner) Replication() error {
	if _, e := r.SQL("SELECT pg_create_physical_replication_slot('kt04_replica') WHERE NOT EXISTS (SELECT FROM pg_replication_slots WHERE slot_name='kt04_replica')", "", "", "", ""); e != nil {
		return e
	}
	if _, e := r.DC("up", "-d", "--wait", "replica"); e != nil {
		return e
	}
	for node, want := range map[string]string{"primary": "f", "replica": "t"} {
		p, e := r.SQL("SELECT pg_is_in_recovery()", node, "", "", "")
		if e != nil {
			return e
		}
		if strings.TrimSpace(p.Out) != want {
			return fmt.Errorf("wrong role for %s", node)
		}
	}
	request := "00000000-0000-4000-8000-000000000004"
	p, e := r.Client("primary", "kt04_app", "release", "--request", request)
	if e != nil {
		return fmt.Errorf("release: %s %w", p.Out, e)
	}
	l, e := r.SQL("SELECT pg_current_wal_lsn()", "", "", "", "")
	if e != nil {
		return e
	}
	lsn := strings.TrimSpace(l.Out)
	if e = r.Replay(lsn); e != nil {
		return e
	}
	a, e := r.SQL("SELECT count(*) FROM transport.operation_request WHERE request_id='"+request+"'", "replica", "kt04_analyst", "", "")
	if e != nil || strings.TrimSpace(a.Out) != "1" {
		return fmt.Errorf("replicated request absent: %v", e)
	}
	if _, e = r.Client("primary", "kt04_app", "release", "--request", request); e != nil {
		return e
	}
	a, e = r.SQL("SELECT count(*) FROM transport_private.duty_status_audit", "", "", "", "")
	if e != nil || strings.TrimSpace(a.Out) != "1" {
		return fmt.Errorf("unexpected audit count: %s %v", a.Out, e)
	}
	denied, e := r.SQL("CREATE TABLE public.forbidden_replica(id int)", "replica", "", "", "")
	if e == nil || !strings.Contains(denied.Err, "25006") {
		return fmt.Errorf("replica allowed write")
	}
	sender, e := r.SQL("SELECT application_name,state,sync_state FROM pg_stat_replication", "", "", "", "")
	if e != nil {
		return e
	}
	receiver, e := r.SQL("SELECT status,sender_host FROM pg_stat_wal_receiver", "replica", "", "", "")
	if e != nil {
		return e
	}
	audit, e := r.SQL("SELECT row_to_json(t) FROM transport_private.duty_status_audit t", "", "", "", "")
	if e != nil {
		return e
	}
	return r.Save("replication", map[string]any{"request": request, "lsn": lsn, "client": p, "sender": sender.Out, "receiver": receiver.Out, "read_only_error": denied.Err, "audit": audit.Out, "primary_in_recovery": false, "replica_in_recovery": true})
}
func (r *Runner) Replay(lsn string) error {
	return Wait(func() (bool, error) {
		p, e := r.SQL("SELECT pg_last_wal_replay_lsn()>='"+lsn+"'::pg_lsn", "replica", "", "", "")
		return strings.TrimSpace(p.Out) == "t", e
	}, 45*time.Second)
}
func (r *Runner) Failures() error {
	var records []map[string]any
	for _, node := range []string{"replica", "primary"} {
		record := map[string]any{"node": node, "started": time.Now().UTC()}
		role := "kt04_app"
		if node == "replica" {
			role = "kt04_analyst"
		}
		// A separate client container stays alive during either database failure.
		watch := make(chan Result, 1)
		go func() { p, _ := r.Client(node, role, "watch", "--iterations", "12", "--interval", "1s"); watch <- p }()
		time.Sleep(1500 * time.Millisecond)
		if _, e := r.DC("stop", node); e != nil {
			return e
		}
		checkErr := func() error {
			start := time.Now()
			p, e := r.Client(node, role, "read")
			record["failure_seconds"] = time.Since(start).Seconds()
			record["failed_read"] = p
			if e == nil || !strings.Contains(p.Out, "connection") {
				return fmt.Errorf("client must report a database connection failure: %s", p.Out)
			}
			if node == "replica" {
				p, e = r.Client("primary", "kt04_app", "read")
				if e != nil {
					return e
				}
				record["primary_read"] = p
				p, e = r.SQL("SELECT transport.release_duty(2,2,'2026-09-22 11:50+07','00000000-0000-4000-8000-000000000005')", "primary", "kt04_app", "", "")
				if e != nil {
					return e
				}
				record["primary_write"] = p.Out
			} else {
				failedWrite, writeErr := r.Client("primary", "kt04_app", "release", "--request", "00000000-0000-4000-8000-000000000004")
				if writeErr == nil {
					return fmt.Errorf("write unexpectedly succeeded while primary was stopped")
				}
				record["failed_write"] = failedWrite
				p, e = r.SQL("SELECT count(id) FROM transport.duty", "replica", "kt04_analyst", "", "")
				if e != nil {
					return e
				}
				record["replica_read"] = p.Out
			}
			return nil
		}()
		start := time.Now()
		if _, e := r.DC("start", node); e != nil {
			return e
		}
		if e := Wait(func() (bool, error) { p, e := r.Client(node, role, "read"); return p.Code == 0, e }, 45*time.Second); e != nil {
			return e
		}
		record["recovery_seconds"] = time.Since(start).Seconds()
		record["continuous_client"] = <-watch
		if checkErr != nil {
			return checkErr
		}
		if _, e := r.SQL("SELECT transport.check_model()", "", "", "", ""); e != nil {
			return e
		}
		l, e := r.SQL("SELECT pg_current_wal_lsn()", "", "", "", "")
		if e != nil {
			return e
		}
		if e = r.Replay(strings.TrimSpace(l.Out)); e != nil {
			return e
		}
		record["integrity"] = "PASS"
		records = append(records, record)
	}
	return r.Save("failures", records)
}
