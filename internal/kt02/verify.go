package kt02

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type race struct {
	name   string
	setup  string
	first  string
	second string
	check  func(firstErr, secondErr error) error
}

func expectError(err error, fragment string) error {
	if err == nil || !strings.Contains(err.Error(), fragment) {
		return fmt.Errorf("expected error containing %q, got %v", fragment, err)
	}
	return nil
}

func (r *runner) scalar(database, statement, want, label string) error {
	got, err := r.sql(database, statement)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if last := lines[len(lines)-1]; last != want {
		return fmt.Errorf("%s: got %q, want %q", label, last, want)
	}
	return nil
}

func (r *runner) runRace(database, first, second string) (error, error) {
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	cmd := r.command(ctx, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-At", "-d", database)
	cmd.Stdin = strings.NewReader("SET application_name='kt02_first'; BEGIN; SET LOCAL search_path=transport,public;\n" + first + "\nSELECT pg_sleep(2); COMMIT;\n")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return err, nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		state, err := r.sqlContext(ctx, database, "SELECT count(*) FROM pg_stat_activity WHERE application_name='kt02_first' AND wait_event='PgSleep';")
		if err != nil {
			cancel()
			<-done
			return err, nil
		}
		if strings.TrimSpace(state) == "1" {
			break
		}
		select {
		case err := <-done:
			return fmt.Errorf("first transaction ended before barrier: %v: %s", err, output.String()), nil
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			return errors.New("first transaction did not reach its barrier"), nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, secondErr := r.sqlContext(ctx, database, "SET search_path=transport,public;\n"+second)
	firstErr := <-done
	if firstErr != nil {
		firstErr = fmt.Errorf("%w: %s", firstErr, output.String())
	}
	return firstErr, secondErr
}

func races() []race {
	release := "SELECT release_duty(1,2,'2026-09-22 09:50+07');"
	return []race{
		{"Two concurrent releases of the same vehicle: exactly one succeeds",
			"UPDATE duty SET vehicle_id=1 WHERE id=3; INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES(4,3,'2026-09-21 12:00+07',1,'Concurrency fixture');",
			release, "SELECT release_duty(3,2,'2026-09-22 09:50+07');",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "one_vehicle_on_line")
			}},
		{"Two concurrent requests with one request id: applied once, both succeed", "",
			"SELECT release_duty(1,2,'2026-09-22 09:50+07','00000000-0000-0000-0000-0000000000b1');",
			"SELECT release_duty(1,2,'2026-09-22 09:50+07','00000000-0000-0000-0000-0000000000b1');",
			func(a, b error) error {
				if a != nil || b != nil {
					return fmt.Errorf("repeated request failed: %v; %v", a, b)
				}
				return nil
			}},
		{"Two concurrent releases of one duty without request id: second rejected", "",
			release, release,
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Only planned duty can be released")
			}},
		{"Two concurrent replacements of one duty: exactly one succeeds", release,
			"SELECT replace_duty(1,3,2,'2026-09-22 09:55+07','Первая подмена');",
			"SELECT replace_duty(1,2,2,'2026-09-22 09:56+07','Вторая подмена');",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Invalid replacement duties")
			}},
		{"Release, then concurrent plan refresh: refresh rejected after waiting", "",
			release, "SELECT refresh_plan(1,1,'2026-09-22 09:55+07','Позднее уточнение');",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Plan already released")
			}},
		{"Plan refresh, then concurrent release: release freezes the refreshed plan",
			"UPDATE route_point SET offset_seconds=1260 WHERE route_id=1 AND point_no=3;",
			"SELECT refresh_plan(1,1,'2026-09-22 08:00+07','Уточнение');", release,
			func(a, b error) error {
				if a != nil || b != nil {
					return fmt.Errorf("refresh/release failed: %v; %v", a, b)
				}
				return nil
			}},
		{"Two concurrent trip starts in one duty: exactly one succeeds", release,
			"UPDATE execution SET status='running',actual_start='2026-09-22 10:00+07',next_point_no=2 WHERE trip_id=1;",
			"UPDATE execution SET status='running',actual_start='2026-09-22 11:00+07',next_point_no=2 WHERE trip_id=2;",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Another running or review trip")
			}},
		{"Duty closure concurrent with trip start: closure rejected", release,
			"UPDATE execution SET status='running',actual_start='2026-09-22 10:00+07',next_point_no=2 WHERE trip_id=1;",
			"SELECT close_duty(1,2,'2026-09-22 10:05+07','Попытка закрытия');",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Unfinished trips block closure")
			}},
		{"Concurrent removal of a user's two roles: write skew rejected", "INSERT INTO user_role VALUES(3,'admin');",
			"DELETE FROM user_role WHERE user_id=3 AND role_code='analyst'; SET CONSTRAINTS ALL IMMEDIATE;",
			"DELETE FROM user_role WHERE user_id=3 AND role_code='admin';",
			func(a, b error) error {
				if a != nil {
					return a
				}
				return expectError(b, "Every user needs at least one role")
			}},
	}
}

var raceResults = map[string][2]string{
	"Two concurrent releases of the same vehicle: exactly one succeeds":         {"SELECT count(*) FROM duty WHERE status='on_line' AND vehicle_id=1;", "1"},
	"Two concurrent requests with one request id: applied once, both succeed":   {"SELECT (SELECT count(*) FROM operation_request)||'/'||(SELECT count(*) FROM duty WHERE status='on_line')||'/'||(SELECT count(*) FROM execution);", "1/1/2"},
	"Two concurrent releases of one duty without request id: second rejected":   {"SELECT count(*) FROM duty WHERE status='on_line';", "1"},
	"Two concurrent replacements of one duty: exactly one succeeds":             {"SELECT string_agg(status,',' ORDER BY id) FROM duty;", "closed,planned,on_line"},
	"Release, then concurrent plan refresh: refresh rejected after waiting":     {"SELECT count(*) FROM planned_trip WHERE refreshed_at IS NOT NULL;", "0"},
	"Plan refresh, then concurrent release: release freezes the refreshed plan": {"SELECT to_char(arrives_at AT TIME ZONE 'Asia/Novosibirsk','HH24:MI')||'/'||(frozen_at IS NOT NULL) FROM trip_point JOIN planned_trip t ON t.id=trip_id WHERE trip_id=1 AND point_no=3;", "10:21/true"},
	"Two concurrent trip starts in one duty: exactly one succeeds":              {"SELECT count(*) FROM execution WHERE status='running';", "1"},
	"Duty closure concurrent with trip start: closure rejected":                 {"SELECT status FROM duty WHERE id=1;", "on_line"},
	"Concurrent removal of a user's two roles: write skew rejected":             {"SELECT count(*) FROM user_role WHERE user_id=3;", "1"},
}

func (r *runner) test() (retErr error) {
	var random [5]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	suffix := hex.EncodeToString(random[:])
	path := filepath.Join(r.root, "submission/kt02-validation.json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var created, events []string
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		for _, db := range created {
			if _, err := r.sqlContext(ctx, "postgres", "DROP DATABASE IF EXISTS "+identifier(db)+" WITH (FORCE);"); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleanup %s: %w", db, err))
			}
		}
		if retErr != nil {
			_ = os.Remove(path)
		}
	}()
	newDatabase := func(prefix string) (string, error) {
		name := fmt.Sprintf("kt02_%s_%d_%s", prefix, len(created), suffix)
		if _, err := r.sql("postgres", "CREATE DATABASE "+identifier(name)+";"); err != nil {
			return "", err
		}
		created = append(created, name)
		return name, nil
	}
	clean, err := newDatabase("clean")
	if err != nil {
		return err
	}
	details := make(map[string]string)
	report := func(db, want string) error {
		got, err := r.fileSQL(db, "sql/report.sql")
		if err != nil {
			return err
		}
		if !strings.Contains(got, want) {
			return fmt.Errorf("unexpected report %q, want %q", strings.TrimSpace(got), want)
		}
		return nil
	}
	type step struct {
		name string
		run  func() error
	}
	steps := []step{
		{"All migrations on a clean database", func() error { return r.migrate(clean, 999, nil) }},
		{"Repeated migration run changes nothing", func() error {
			before, err := r.signature(clean)
			if err != nil {
				return err
			}
			if err = r.migrate(clean, 999, nil); err != nil {
				return err
			}
			after, err := r.signature(clean)
			if err != nil {
				return err
			}
			return same(before, after, "repeat migration")
		}},
		{"Changed SQL of an applied migration rejected by SHA-256", func() error {
			err := r.migrate(clean, 999, func(v int, c []byte) []byte {
				if v == 1 {
					return append(c, []byte("\nSELECT 1;\n")...)
				}
				return c
			})
			return expectError(err, "checksum mismatch")
		}},
		{"Comment and whitespace edits of applied migrations accepted", func() error {
			return r.migrate(clean, 999, func(v int, c []byte) []byte {
				return append([]byte("-- revised note\n\n"), bytes.ReplaceAll(c, []byte("\n"), []byte("  \n"))...)
			})
		}},
		{"Registry of the previous commented release upgrades to the latest schema", func() error {
			db, err := newDatabase("legacy")
			if err != nil {
				return err
			}
			if err = r.migrate(db, 2, nil); err != nil {
				return err
			}
			legacy, err := legacyChecksums()
			if err != nil {
				return err
			}
			for version := 1; version <= 2; version++ {
				if _, err = r.sql(db, fmt.Sprintf("UPDATE public.schema_migrations SET sha256=%s WHERE version=%d;", literal(legacy[strconv.Itoa(version)][0]), version)); err != nil {
					return err
				}
			}
			if err = r.migrate(db, 999, nil); err != nil {
				return err
			}
			a, err := r.signature(db)
			if err != nil {
				return err
			}
			b, err := r.signature(clean)
			if err != nil {
				return err
			}
			if err = same(a, b, "legacy upgrade schema"); err != nil {
				return err
			}
			if _, err = r.sql(db, "INSERT INTO public.schema_migrations(version,name,sha256) VALUES(999,'999_future.sql','x');"); err != nil {
				return err
			}
			return expectError(r.migrate(db, 999, nil), "unknown to this release")
		}},
		{"Upgrade 002 -> 003 -> 004 with lifecycle data: rows kept, schema equals clean, results unchanged", func() error {
			db, err := newDatabase("upgrade")
			if err != nil {
				return err
			}
			if err = r.migrate(db, 2, nil); err != nil {
				return err
			}
			if _, err = r.fileSQL(db, "sql/seed.sql"); err != nil {
				return err
			}
			for _, stage := range []struct {
				target int
				before string
			}{{3, ""}, {999, "sql/lifecycles.sql"}} {
				if stage.before != "" {
					if _, err = r.fileSQL(db, stage.before); err != nil {
						return err
					}
				}
				tables, err := r.tables(db)
				if err != nil {
					return err
				}
				before, err := r.snapshot(db, tables)
				if err != nil {
					return err
				}
				if err = r.migrate(db, stage.target, nil); err != nil {
					return err
				}
				after, err := r.snapshot(db, tables)
				if err != nil {
					return err
				}
				if err = same(before, after, fmt.Sprintf("upgrade data to %d", stage.target)); err != nil {
					return err
				}
			}
			a, err := r.signature(db)
			if err != nil {
				return err
			}
			b, err := r.signature(clean)
			if err != nil {
				return err
			}
			if err = same(a, b, "upgrade schema"); err != nil {
				return err
			}
			if _, err = r.fileSQL(db, "tests/assert_demo.sql"); err != nil {
				return err
			}
			return report(db, "4|2|1|1|0|0|0|1|12|2|2|100.0|1|0")
		}},
	}
	for _, rc := range races() {
		rc := rc
		steps = append(steps, step{rc.name, func() error {
			db, err := newDatabase("race")
			if err != nil {
				return err
			}
			if err = r.migrate(db, 999, nil); err != nil {
				return err
			}
			if _, err = r.fileSQL(db, "sql/seed.sql"); err != nil {
				return err
			}
			if rc.setup != "" {
				if _, err = r.sql(db, "BEGIN; SET LOCAL search_path=transport,public; SET LOCAL TIME ZONE 'Asia/Novosibirsk';\n"+rc.setup+"\nCOMMIT;"); err != nil {
					return err
				}
			}
			a, b := r.runRace(db, rc.first, rc.second)
			if err = rc.check(a, b); err != nil {
				return err
			}
			result := raceResults[rc.name]
			return r.scalar(db, "SET search_path=transport,public;\n"+result[0], result[1], rc.name)
		}})
	}
	steps = append(steps,
		step{"Consistent seed committed with deferred constraints", func() error { _, err := r.fileSQL(clean, "sql/seed.sql"); return err }},
		step{"Report before release: N=0 gives NULL share", func() error { return report(clean, "4|0|0|0|0|0|0|4|12|0|0||0|0") }},
		step{"Negative constraints and rollback", func() error {
			result, err := r.fileSQL(clean, "tests/negative.sql")
			if err == nil {
				var labels []string
				for _, line := range strings.Split(result, "\n") {
					if strings.HasSuffix(line, "]") {
						labels = append(labels, line)
					}
				}
				details["Negative constraints and rollback"] = fmt.Sprintf("%d rejections with expected SQLSTATE and constraint or message: %s", len(labels), strings.Join(labels, "; "))
			}
			return err
		}},
		step{"Boundary and plan snapshot checks", func() error { _, err := r.fileSQL(clean, "tests/positive.sql"); return err }},
		step{"Operations: plan refresh, replacements, cancellation, trip copy, request ids, closure, report filter", func() error {
			_, err := r.fileSQL(clean, "tests/operations.sql")
			return err
		}},
		step{"Application and telemetry roles cannot bypass operations", func() error {
			_, err := r.fileSQL(clean, "tests/privileges.sql")
			return err
		}},
		step{"Duty, execution and deviation lifecycles with asserted results", func() error {
			if _, err := r.fileSQL(clean, "sql/lifecycles.sql"); err != nil {
				return err
			}
			_, err := r.fileSQL(clean, "tests/assert_demo.sql")
			return err
		}},
		step{"Report: 4 trips; 2 done (1 by dispatcher); 1 failed; 1 unreleased; K=2 N=2; 1 unreliable; 0 open deviations", func() error {
			return report(clean, "4|2|1|1|0|0|0|1|12|2|2|100.0|1|0")
		}},
	)
	for _, step := range steps {
		if err := step.run(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
		name := step.name
		if detail := details[name]; detail != "" {
			name += ": " + detail
		}
		events = append(events, name)
		fmt.Fprintln(r.out, "PASS "+name)
	}
	server, err := r.sql(clean, "SELECT version();")
	if err != nil {
		return err
	}
	return writeJSON(path, validation{Status: "PASS", Server: strings.TrimSpace(server), CheckedAt: time.Now().UTC().Format(time.RFC3339), Checks: events})
}
