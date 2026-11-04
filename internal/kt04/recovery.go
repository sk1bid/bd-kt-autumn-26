package kt04

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func recoveryResult(p Result, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if p.Code != 0 {
		return "", fmt.Errorf("command failed (%d): %s", p.Code, strings.TrimSpace(p.Err))
	}
	return p.Out, nil
}

func (r *Runner) recoverySQL(query, service, role, database string) (string, error) {
	out, err := recoveryResult(r.SQL(query, service, role, database, "verify-full"))
	return strings.TrimSpace(out), err
}

func (r *Runner) recoverySnapshot(service string) (map[string]any, error) {
	const schemas = "('transport','transport_private')"
	query := "SELECT quote_ident(schemaname)||'.'||quote_ident(tablename) FROM pg_tables WHERE schemaname IN " + schemas + " ORDER BY 1"
	tables, err := r.recoverySQL(query, service, "postgres", DB)
	if err != nil {
		return nil, err
	}
	if tables == "" {
		return nil, errors.New("backup snapshot has no application tables")
	}
	snapshot := make(map[string]any)
	for _, table := range strings.Split(tables, "\n") {
		out, err := recoveryResult(r.SQL("SELECT row_to_json(t)::text FROM "+table+" t ORDER BY row_to_json(t)::text", service, "postgres", DB, "verify-full"))
		if err != nil {
			return nil, err
		}
		snapshot[table] = map[string]any{
			"rows":   strings.Count(out, "\n"),
			"sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(out))),
		}
	}
	queries := map[string]string{
		"sequences": "SELECT schemaname,sequencename,last_value FROM pg_sequences WHERE schemaname IN " + schemas + " ORDER BY 1,2",
		"owners_acl": "SELECT n.nspname,c.relname,pg_get_userbyid(c.relowner)," +
			"coalesce((SELECT string_agg(entry::text,',' ORDER BY entry::text) FROM unnest(c.relacl) AS entry),'')" +
			" FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN " + schemas + " ORDER BY 1,2",
		"database_acl": "SELECT pg_get_userbyid(datdba), coalesce((SELECT string_agg(entry::text,',' ORDER BY entry::text)" +
			" FROM unnest(datacl) AS entry),'') FROM pg_database WHERE datname=current_database()",
		"migrations": "SELECT version,name,sha256 FROM public.schema_migrations ORDER BY version",
	}
	for key, query := range queries {
		out, err := r.recoverySQL(query, service, "postgres", DB)
		if err != nil {
			return nil, err
		}
		snapshot[key] = out
	}
	return snapshot, nil
}

func (r *Runner) createRecoveryDump(dump string) ([32]byte, error) {
	var digest [32]byte
	staged, err := os.CreateTemp(filepath.Dir(dump), ".kt04-dump-*")
	if err != nil {
		return digest, err
	}
	defer os.Remove(staged.Name())
	archive, err := recoveryResult(r.DC("exec", "-T", "-u", "postgres", "primary", "pg_dump", "-Fc", "-d", DB))
	if err != nil {
		staged.Close()
		return digest, err
	}
	if _, err := staged.WriteString(archive); err != nil {
		staged.Close()
		return digest, err
	}
	if err := staged.Close(); err != nil {
		return digest, err
	}
	if err := os.Link(staged.Name(), dump); err != nil {
		return digest, fmt.Errorf("publish backup without replacing an existing file: %w", err)
	}
	return sha256.Sum256([]byte(archive)), nil
}

func (r *Runner) Backup() error {
	if !filepath.IsAbs(r.BackupDir) {
		return errors.New("KT04_BACKUP_DIR must be absolute")
	}
	// Resolve the deepest existing parent before creating a directory.
	parent := filepath.Clean(r.BackupDir)
	for {
		if _, err := os.Lstat(parent); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent = filepath.Dir(parent)
	}
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	suffix, err := filepath.Rel(parent, r.BackupDir)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(canonical, suffix)
	root, err := filepath.EvalSymlinks(r.Root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, backupDir)
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("backup directory must be outside the repository")
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	metadata, err := os.Stat(backupDir)
	if err != nil {
		return err
	}
	if !metadata.IsDir() || metadata.Mode().Perm() != 0700 {
		return errors.New("backup directory must have mode 0700")
	}
	dump := filepath.Join(backupDir, "transport.dump")
	if _, err := os.Lstat(dump); err == nil {
		return errors.New("backup already exists; choose a new KT04_BACKUP_DIR")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if _, err := recoveryResult(r.DC("--profile", "restore", "up", "-d", "--wait", "restore")); err != nil {
		return err
	}
	out, err := r.recoverySQL(
		"SELECT EXISTS(SELECT FROM pg_database WHERE datname='"+DB+"') OR EXISTS(SELECT FROM pg_roles WHERE rolname='kt04_app')",
		"restore", "postgres", "postgres")
	if err != nil {
		return err
	}
	if out != "f" {
		return errors.New("restore already has KT-04 data or roles; existing state has been preserved")
	}
	before, err := r.recoverySnapshot("primary")
	if err != nil {
		return err
	}
	digest, err := r.createRecoveryDump(dump)
	if err != nil {
		return err
	}

	roleSQL, err := recoveryResult(r.DC("exec", "-T", "-u", "postgres", "primary", "pg_dumpall", "--roles-only", "--no-role-passwords"))
	if err != nil {
		return err
	}
	roleSQL = strings.Replace(roleSQL, "CREATE ROLE postgres;\n", "", 1)
	if _, err := r.recoverySQL(roleSQL, "restore", "postgres", "postgres"); err != nil {
		return err
	}
	if err := r.Passwords("restore"); err != nil {
		return err
	}

	tmp, err := recoveryResult(r.DC("exec", "-T", "restore", "mktemp", "/tmp/kt04-restore.XXXXXX"))
	if err != nil {
		return err
	}
	tmp = strings.TrimSpace(tmp)
	defer r.DC("exec", "-T", "restore", "rm", "-f", tmp)
	if _, err := recoveryResult(r.DC("--profile", "restore", "cp", dump, "restore:"+tmp)); err != nil {
		return err
	}
	if _, err := recoveryResult(r.DC("exec", "-T", "restore", "chown", "postgres:postgres", tmp)); err != nil {
		return err
	}
	// --create applies the archived database-level owner, settings, and ACL.
	if _, err := recoveryResult(r.DC("exec", "-T", "-u", "postgres", "restore", "pg_restore", "--exit-on-error", "--create", "-d", "postgres", tmp)); err != nil {
		return err
	}
	after, err := r.recoverySnapshot("restore")
	if err != nil {
		return err
	}
	for key, value := range before {
		if !reflect.DeepEqual(value, after[key]) {
			return fmt.Errorf("restored snapshot differs at %s", key)
		}
	}
	if _, err := r.recoverySQL("SELECT transport.check_model()", "restore", "postgres", DB); err != nil {
		return err
	}
	for _, role := range []string{"kt04_admin", "kt04_app", "kt04_analyst"} {
		out, err := r.recoverySQL("SELECT session_user,count(id) FROM transport.duty", "restore", role, DB)
		if err != nil || out != role+"|"+restoredDutyCount(before) {
			return fmt.Errorf("restored LOGIN or duty count differs for %s: %q (%v)", role, out, err)
		}
	}
	denied, _ := r.SQL("CREATE ROLE forbidden_kt04_restore", "restore", "kt04_app", DB, "verify-full")
	if denied.Code == 0 || !strings.Contains(denied.Err, "42501") {
		return errors.New("restored app role did not reject CREATE ROLE with 42501")
	}
	const identity = `BEGIN;
INSERT INTO transport.vehicle(registration_no,transport_kind,capacity,admitted_until)
VALUES('RESTORE-CHECK','bus',10,'2026-12-31')
RETURNING id > coalesce((SELECT max(id) FROM transport.vehicle), 0);
ROLLBACK;`
	if out, err := r.recoverySQL(identity, "restore", "postgres", DB); err != nil || !containsOutputLine(out, "t") {
		return fmt.Errorf("restored identity sequence check failed: %q (%v)", out, err)
	}
	invalid, _ := r.SQL("INSERT INTO transport.vehicle(registration_no,transport_kind,capacity,admitted_until) VALUES('BAD','bus',-1,'2026-12-31')", "restore", "postgres", DB, "verify-full")
	if invalid.Code == 0 || !strings.Contains(invalid.Err, "23514") {
		return errors.New("restored capacity constraint did not reject negative capacity with 23514")
	}
	still, err := r.recoverySnapshot("primary")
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, still) {
		return errors.New("primary snapshot changed during restore")
	}
	return r.Save("restore", map[string]any{
		"source": before, "restored": after, "identical": true, "model": "PASS",
		"role_login_and_denial": "PASS", "identity_and_constraint": "PASS",
		"dump_sha256": fmt.Sprintf("%x", digest),
	})
}

func restoredDutyCount(snapshot map[string]any) string {
	rows, _ := snapshot["transport.duty"].(map[string]any)
	return fmt.Sprint(rows["rows"])
}

func containsOutputLine(out, wanted string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == wanted {
			return true
		}
	}
	return false
}

type lagSample struct {
	Time        time.Time `json:"time"`
	ReplayBytes int64     `json:"replay_bytes"`
}

func (r *Runner) Lag() error {
	const setup = `BEGIN;
CREATE SCHEMA IF NOT EXISTS kt04_lab;
CREATE TABLE kt04_lab.bulk_messages(id integer PRIMARY KEY, payload text NOT NULL);
COMMIT;`
	if _, err := r.recoverySQL(setup, "primary", "postgres", DB); err != nil {
		return err
	}
	const sampleSQL = `SELECT greatest(0, pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn))::bigint
FROM pg_stat_replication WHERE application_name='kt04_replica' AND state='streaming' AND replay_lsn IS NOT NULL`
	samples := []lagSample{}
	sample := func() error {
		out, err := r.recoverySQL(sampleSQL, "primary", "postgres", DB)
		if err != nil {
			return err
		}
		n, err := strconv.ParseInt(out, 10, 64)
		if err != nil {
			return fmt.Errorf("expected one streaming replica and replay position: %w", err)
		}
		samples = append(samples, lagSample{Time: time.Now().UTC(), ReplayBytes: n})
		return nil
	}
	if err := sample(); err != nil {
		return err
	}
	const load = `INSERT INTO kt04_lab.bulk_messages(id,payload)
SELECT i, repeat(md5(i::text), 16) FROM generate_series(1,100000) AS g(i)`
	loadResult := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := r.recoverySQL(load, "primary", "postgres", DB)
		loadResult <- err
	}()
	var sampleErr error
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	loading := true
	for loading {
		select {
		case err := <-loadResult:
			if err != nil {
				return err
			}
			loading = false
		case <-ticker.C:
			if sampleErr == nil {
				sampleErr = sample()
			}
		}
	}
	if sampleErr != nil {
		return sampleErr
	}
	loadElapsed := time.Since(started)
	sourceLSN, err := r.recoverySQL("SELECT pg_current_wal_lsn()", "primary", "postgres", DB)
	if err != nil {
		return err
	}
	if !walLSNPattern.MatchString(sourceLSN) {
		return fmt.Errorf("unexpected source WAL position %q", sourceLSN)
	}
	visibleStarted := time.Now()
	visible := func() (bool, error) {
		if err := sample(); err != nil {
			return false, err
		}
		out, err := r.recoverySQL("SELECT pg_last_wal_replay_lsn()>='"+sourceLSN+"'::pg_lsn", "replica", "postgres", DB)
		if err != nil || out != "t" {
			return false, err
		}
		out, err = r.recoverySQL("SELECT count(*),count(*) FILTER (WHERE id=100000) FROM kt04_lab.bulk_messages", "replica", "postgres", DB)
		return out == "100000|1", err
	}
	if err := Wait(visible, 45*time.Second); err != nil {
		return err
	}
	var peak int64
	for _, s := range samples {
		peak = max(peak, s.ReplayBytes)
	}
	return r.Save("lag", map[string]any{
		"rows": 100000, "marker_id": 100000, "marker_visible_on_replica": true,
		"primary_lsn": sourceLSN, "peak_replay_bytes": peak, "samples": samples,
		"load_ms": loadElapsed.Milliseconds(), "catchup_check_ms": time.Since(visibleStarted).Milliseconds(),
	})
}

var (
	walLSNPattern  = regexp.MustCompile(`^[0-9A-F]+/[0-9A-F]+$`)
	walFilePattern = regexp.MustCompile(`^[0-9A-F]{24}$`)
)

const pitrBaseBackup = `set -eu
mkdir "$PGDATA"
trap 'rmdir "$PGDATA" 2>/dev/null || true' EXIT
chown postgres:postgres "$PGDATA"
chmod 0700 "$PGDATA"
install -o postgres -g postgres -m 0600 /run/secrets/kt04_replicator.pgpass /run/kt04/pitr.pgpass
install -m 0644 /run/secrets/ca.crt /run/kt04/ca.crt
gosu postgres pg_basebackup --pgdata="$PGDATA" --wal-method=stream --checkpoint=fast --no-password --no-clean \
  --dbname="host=primary port=5432 user=kt04_replicator application_name=kt04_pitr connect_timeout=5 sslmode=verify-full sslrootcert=/run/kt04/ca.crt passfile=/run/kt04/pitr.pgpass"
gosu postgres pg_verifybackup "$PGDATA"
`

const pitrRecoveryConfig = `set -eu
test -s "$PGDATA/PG_VERSION"
test ! -e "$PGDATA/recovery.signal"
printf "%s\n" \
  "restore_command = 'cp /wal-archive/%f %p'" \
  "recovery_target_time = '$1'" \
  "recovery_target_timeline = 'current'" \
  "recovery_target_action = 'promote'" >> "$PGDATA/postgresql.auto.conf"
gosu postgres touch "$PGDATA/recovery.signal"
`

func (r *Runner) PITR() error {
	if _, err := recoveryResult(r.DC("--profile", "pitr", "run", "--rm", "--no-deps", "--entrypoint", "sh", "pitr", "-c", pitrBaseBackup)); err != nil {
		return fmt.Errorf("create verified physical backup in fresh PITR volume: %w", err)
	}
	const before = `BEGIN;
CREATE SCHEMA IF NOT EXISTS kt04_lab;
CREATE TABLE kt04_lab.pitr_markers(name text PRIMARY KEY);
INSERT INTO kt04_lab.pitr_markers VALUES ('before-target');
COMMIT;`
	if _, err := r.recoverySQL(before, "primary", "postgres", DB); err != nil {
		return err
	}
	target, err := r.recoverySQL(`SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US')||'+00:00'`, "primary", "postgres", DB)
	if err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, target); err != nil {
		return fmt.Errorf("parse PostgreSQL recovery target: %w", err)
	}
	if _, err := r.recoverySQL("INSERT INTO kt04_lab.pitr_markers VALUES ('after-target')", "primary", "postgres", DB); err != nil {
		return err
	}
	walFile, err := r.recoverySQL("SELECT pg_walfile_name(pg_current_wal_lsn())", "primary", "postgres", DB)
	if err != nil {
		return err
	}
	if !walFilePattern.MatchString(walFile) {
		return fmt.Errorf("unexpected WAL archive filename %q", walFile)
	}
	if _, err := r.recoverySQL("SELECT pg_switch_wal()", "primary", "postgres", DB); err != nil {
		return err
	}
	archived := func() (bool, error) {
		p, err := r.DC("exec", "-T", "primary", "test", "-s", "/wal-archive/"+walFile)
		return p.Code == 0 && err == nil, nil
	}
	if err := Wait(archived, 45*time.Second); err != nil {
		return fmt.Errorf("archive WAL containing after-target marker: %w", err)
	}
	if _, err := recoveryResult(r.DC("--profile", "pitr", "run", "--rm", "--no-deps", "--entrypoint", "sh", "pitr", "-c", pitrRecoveryConfig, "sh", target)); err != nil {
		return err
	}
	if _, err := recoveryResult(r.DC("--profile", "pitr", "up", "-d", "--wait", "pitr")); err != nil {
		return err
	}
	promoted := func() (bool, error) {
		out, err := r.recoverySQL("SELECT NOT pg_is_in_recovery()", "pitr", "postgres", DB)
		return out == "t", err
	}
	if err := Wait(promoted, 45*time.Second); err != nil {
		return fmt.Errorf("wait for PITR promotion: %w", err)
	}
	const markers = "SELECT name FROM kt04_lab.pitr_markers ORDER BY name"
	source, err := r.recoverySQL(markers, "primary", "postgres", DB)
	if err != nil {
		return err
	}
	recovered, err := r.recoverySQL(markers, "pitr", "postgres", DB)
	if err != nil {
		return err
	}
	if source != "after-target\nbefore-target" || recovered != "before-target" {
		return fmt.Errorf("PITR marker mismatch: source %q, recovered %q", source, recovered)
	}
	if _, err := r.recoverySQL("SELECT transport.check_model()", "pitr", "postgres", DB); err != nil {
		return err
	}
	replayed, err := r.recoverySQL("SELECT pg_last_xact_replay_timestamp()", "pitr", "postgres", DB)
	if err != nil {
		return err
	}
	return r.Save("pitr", map[string]any{
		"target_utc": target, "archived_wal": walFile, "base_backup_verified": true,
		"promoted": true, "primary_markers": strings.Split(source, "\n"),
		"recovered_markers": strings.Split(recovered, "\n"), "model": "PASS",
		"last_replayed_commit": replayed,
	})
}
