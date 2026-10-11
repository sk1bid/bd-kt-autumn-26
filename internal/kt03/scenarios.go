package kt03

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *runner) snapshot(db string) (json.RawMessage, error) {
	out, err := r.sql(db, `SELECT jsonb_build_object(
 'duty',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM duty x),
 'assignment',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM assignment x),
 'execution',(SELECT jsonb_agg(to_jsonb(x) ORDER BY trip_id) FROM execution x),
 'planned_trip',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM planned_trip x),
 'operation_request',(SELECT jsonb_agg(to_jsonb(x) ORDER BY request_id) FROM operation_request x));`)
	return json.RawMessage(strings.TrimSpace(out)), err
}
func (r *runner) scenarioFile(db, name string) error {
	b, err := os.ReadFile(filepath.Join(r.root, "sql/kt03/scenarios", name+".sql"))
	if err != nil {
		return err
	}
	c := r.command(db)
	c.Stdin = strings.NewReader("SET search_path=transport,public; SET TIME ZONE 'Asia/Novosibirsk';\n" + string(b))
	out, err := c.CombinedOutput()
	if e := r.saveText("scenarios/"+name+".txt", string(out)); e != nil {
		return e
	}
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, out)
	}
	return nil
}
func (r *runner) scenarios() error {
	if err := r.invalidate("scenarios/commit.json", "scenarios/rollback.json", "scenarios/concurrency.json", "business-regression.json"); err != nil {
		return err
	}
	db := "transport_kt03_scenarios"
	defer r.sql("postgres", "DROP DATABASE IF EXISTS "+db+";")
	if err := r.reset(db, 5); err != nil {
		return err
	}
	if _, err := r.file(db, "sql/seed.sql"); err != nil {
		return err
	}
	if err := r.testBusinessChecks(db); err != nil {
		return err
	}
	validation, err := r.file(db, "tests/kt03/batch_validation.sql")
	if err != nil {
		return err
	}
	if err = r.saveText("batch-validation.txt", validation); err != nil {
		return err
	}
	if _, err := r.sql(db, "SELECT release_duty(1,2,'2026-09-22 09:50+07');"); err != nil {
		return err
	}
	before, err := r.snapshot(db)
	if err != nil {
		return err
	}
	if err = r.scenarioFile(db, "commit"); err != nil {
		return err
	}
	after, err := r.snapshot(db)
	if err != nil {
		return err
	}
	if err = r.save("scenarios/commit.json", map[string]any{"status": "PASS", "before": before, "after": after}); err != nil {
		return err
	}
	if _, err = r.sql(db, "UPDATE vehicle SET admitted_until='2026-09-21' WHERE id=2;"); err != nil {
		return err
	}
	before, err = r.snapshot(db)
	if err != nil {
		return err
	}
	if err = r.scenarioFile(db, "rollback"); err != nil {
		return err
	}
	after, err = r.snapshot(db)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return fmt.Errorf("rollback leaked business changes")
	}
	if err = r.save("scenarios/rollback.json", map[string]any{"status": "PASS", "all_related_rows_unchanged": true, "before": before, "after": after, "sequence_values_excluded": "PostgreSQL sequences are not transactional"}); err != nil {
		return err
	}
	if err = r.reset(db, 5); err != nil {
		return err
	}
	if _, err = r.file(db, "sql/seed.sql"); err != nil {
		return err
	}
	if _, err = r.sql(db, `BEGIN; UPDATE duty SET vehicle_id=1 WHERE id=3;
INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES(4,3,'2026-09-21 12:00+07',1,'Concurrent release fixture'); COMMIT;`); err != nil {
		return err
	}
	return r.lockScenario(db)
}
func (r *runner) waitFor(db, query string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := r.sql(db, query)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "t" {
			return nil
		}
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return fmt.Errorf("synchronization condition not reached: %s", query)
}
func (r *runner) lockScenario(db string) error {
	first := r.command(db)
	in, err := first.StdinPipe()
	if err != nil {
		return err
	}
	var firstOut bytes.Buffer
	first.Stdout = &firstOut
	first.Stderr = &firstOut
	if err = first.Start(); err != nil {
		return err
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	finished := false
	defer func() {
		if !finished {
			in.Close()
			first.Process.Kill()
			<-firstDone
		}
	}()
	_, err = io.WriteString(in, `SET application_name='kt03_first'; SET search_path=transport,public;
SET idle_in_transaction_session_timeout='30s'; BEGIN;
SELECT release_duty(1,2,'2026-09-22 09:50+07');
`)
	if err != nil {
		return err
	}
	if err = r.waitFor(db, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='kt03_first' AND state='idle in transaction' AND query LIKE '%release_duty(1,%');"); err != nil {
		return err
	}
	second := r.command(db)
	second.Stdin = strings.NewReader(`SET application_name='kt03_second'; SET search_path=transport,public;
SET statement_timeout='25s'; BEGIN; SELECT release_duty(3,2,'2026-09-22 09:50+07'); COMMIT;`)
	var secondOut bytes.Buffer
	second.Stdout = &secondOut
	second.Stderr = &secondOut
	if err = second.Start(); err != nil {
		return err
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Wait() }()
	secondFinished := false
	defer func() {
		if !secondFinished {
			second.Process.Kill()
			<-secondDone
		}
	}()
	if err = r.waitFor(db, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='kt03_second' AND wait_event_type='Lock' AND cardinality(pg_blocking_pids(pid))>0);"); err != nil {
		return err
	}
	locks, err := r.file(db, "sql/kt03/scenarios/locks.sql")
	if err != nil {
		return err
	}
	var diagnostics []json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(locks), "\n") {
		diagnostics = append(diagnostics, json.RawMessage(line))
	}
	if err = r.save("scenarios/lock-wait.json", diagnostics); err != nil {
		return err
	}
	if _, err = io.WriteString(in, "COMMIT;\n"); err != nil {
		return err
	}
	in.Close()
	err = <-firstDone
	finished = true
	if err != nil {
		return fmt.Errorf("first session: %w: %s", err, firstOut.String())
	}
	secondErr := <-secondDone
	secondFinished = true
	if secondErr == nil || !strings.Contains(secondOut.String(), "one_vehicle_on_line") || !strings.Contains(secondOut.String(), "23505") {
		return fmt.Errorf("unexpected second result: %v: %s", secondErr, secondOut.String())
	}
	if err = r.saveText("scenarios/session-first.txt", firstOut.String()); err != nil {
		return err
	}
	if err = r.saveText("scenarios/session-second.txt", secondOut.String()); err != nil {
		return err
	}
	count, err := r.sql(db, "SELECT count(*) FROM duty WHERE vehicle_id=1 AND status='on_line';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(count) != "1" {
		return fmt.Errorf("concurrent release broke vehicle rule")
	}
	snapshot, err := r.snapshot(db)
	if err != nil {
		return err
	}
	return r.save("scenarios/concurrency.json", map[string]any{"status": "PASS", "first": "COMMIT", "second": "23505 one_vehicle_on_line; transaction aborted", "active_duties_for_vehicle": 1, "snapshot": snapshot})
}
