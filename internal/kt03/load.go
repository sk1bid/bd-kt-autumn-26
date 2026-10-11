package kt03

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *runner) readManifest() (manifest, error) {
	var m manifest
	b, err := os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != generatorVersion || len(m.Tables) != 19 {
		return m, fmt.Errorf("unsupported dataset manifest")
	}
	for _, t := range m.Tables {
		for _, s := range []string{t.Table, t.Columns} {
			for _, c := range s {
				if c != '_' && c != ',' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
					return m, fmt.Errorf("invalid table/column name")
				}
			}
		}
		f, err := os.Open(filepath.Join(r.data, t.Table+".csv"))
		if err != nil {
			return m, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return m, err
		}
		if hex.EncodeToString(h.Sum(nil)) != t.SHA256 {
			return m, fmt.Errorf("checksum mismatch: %s", t.Table)
		}
	}
	return m, nil
}

func (r *runner) streamLoad(w io.Writer, m manifest, onlyMessages bool) error {
	b := bufio.NewWriterSize(w, 1<<20)
	fmt.Fprintln(b, "SET search_path=transport,public; SET TIME ZONE 'Asia/Novosibirsk'; SET statement_timeout='10min'; BEGIN;")
	for _, t := range m.Tables {
		if onlyMessages && t.Table != "message_key" {
			continue
		}
		f, err := os.Open(filepath.Join(r.data, t.Table+".csv"))
		if err != nil {
			return err
		}
		if r.method == "copy" {
			fmt.Fprintf(b, "COPY %s(%s) FROM STDIN WITH (FORMAT csv, NULL '\\N');\n", t.Table, t.Columns)
			_, err = io.Copy(b, f)
			fmt.Fprintln(b, `\.`)
		} else {
			rd := csv.NewReader(f)
			count := 0
			for {
				row, e := rd.Read()
				if e == io.EOF {
					break
				}
				if e != nil {
					err = e
					break
				}
				if count%1000 == 0 {
					fmt.Fprintf(b, "INSERT INTO %s(%s) OVERRIDING SYSTEM VALUE VALUES\n", t.Table, t.Columns)
				} else {
					fmt.Fprintln(b, ",")
				}
				for i, s := range row {
					if s != null {
						row[i] = lit(s)
					} else {
						row[i] = "NULL"
					}
				}
				fmt.Fprintf(b, "(%s)", strings.Join(row, ","))
				count++
				if count%1000 == 0 {
					fmt.Fprintln(b, ";")
				}
			}
			if count%1000 != 0 {
				fmt.Fprintln(b, ";")
			}
		}
		f.Close()
		if err != nil {
			return err
		}
	}
	if !onlyMessages {
		fmt.Fprintln(b, `UPDATE planned_trip t SET frozen_at=d.released_at FROM assignment a JOIN duty d ON d.id=a.duty_id JOIN execution e ON e.trip_id=a.trip_id WHERE t.id=a.trip_id;
DO $$ DECLARE r record; BEGIN
 FOR r IN SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='transport' AND is_identity='YES' LOOP
  EXECUTE format('SELECT setval(pg_get_serial_sequence(%L,%L),coalesce(max(%I),1),max(%I) IS NOT NULL) FROM transport.%I','transport.'||r.table_name,r.column_name,r.column_name,r.column_name,r.table_name);
 END LOOP;
END $$;`)
	}
	fmt.Fprintln(b, "COMMIT;")
	return b.Flush()
}

func (r *runner) loadStream(m manifest, onlyMessages bool) (float64, error) {
	in, out := io.Pipe()
	done := make(chan error, 1)
	go func() { err := r.streamLoad(out, m, onlyMessages); out.CloseWithError(err); done <- err }()
	start := time.Now()
	_, err := r.input(r.db, in)
	in.Close()
	streamErr := <-done
	if err != nil {
		return ms(start), err
	}
	return ms(start), streamErr
}

func (r *runner) load() error {
	if err := r.invalidate("verification-dev.json", "verification-load.json"); err != nil {
		return err
	}
	m, err := r.readManifest()
	if err != nil {
		return err
	}
	if err = r.reset(r.db, 5); err != nil {
		return err
	}
	elapsed, err := r.loadStream(m, false)
	if err != nil {
		return err
	}
	fmt.Printf("Loaded %s with %s: %.1f ms\n", r.db, r.method, elapsed)
	if _, err = r.sql(r.db, "ANALYZE;"); err != nil {
		return err
	}
	if err = r.save("load-"+m.Mode+".json", map[string]any{"database": r.db, "method": r.method, "wall_ms_including_commit": elapsed, "manifest": m}); err != nil {
		return err
	}
	return r.verify()
}

func (r *runner) verify() error {
	if err := r.invalidate("verification-dev.json", "verification-load.json"); err != nil {
		return err
	}
	m, err := r.readManifest()
	if err != nil {
		return err
	}
	businessChecks, err := r.businessChecks(r.db)
	if err != nil {
		return err
	}
	for name, count := range businessChecks {
		if count != 0 {
			return fmt.Errorf("business rule %s: %d violations", name, count)
		}
	}
	for _, t := range m.Tables {
		got, err := r.sql(r.db, "SELECT count(*) FROM "+t.Table+";")
		if err != nil {
			return err
		}
		if strings.TrimSpace(got) != fmt.Sprint(t.Rows) {
			return fmt.Errorf("row count %s: got %s want %d", t.Table, got, t.Rows)
		}
	}
	checks, err := r.sql(r.db, fmt.Sprintf(`SELECT
 (SELECT count(*) FROM passage p LEFT JOIN message_key k ON (k.device_id,k.message_id)=(p.source_device_id,p.source_message_id)
  WHERE k.result IS DISTINCT FROM 'accepted' OR k.generated_at IS DISTINCT FROM p.arrived_at),
 (SELECT count(*) FROM message_key WHERE first_received_at>%s::timestamptz OR generated_at>first_received_at),
 (SELECT count(*) FROM execution WHERE actual_start>%s::timestamptz OR actual_end>%s::timestamptz),
 (SELECT count(*) FROM duty d WHERE d.status='closed' AND d.unchecked_results IS DISTINCT FROM EXISTS(
  SELECT 1 FROM assignment a JOIN execution e ON e.trip_id=a.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND e.status='review')),
 (SELECT count(*) FROM pg_constraint WHERE connamespace='transport'::regnamespace AND NOT convalidated),
 (SELECT count(*) FROM pg_trigger WHERE tgrelid IN (SELECT relid FROM pg_stat_user_tables WHERE schemaname IN ('transport','transport_private')) AND tgenabled='D');`, lit(m.Anchor), lit(m.Anchor), lit(m.Anchor)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(checks) != "0|0|0|0|0|0" {
		return fmt.Errorf("generated history validation failed: %s", checks)
	}
	out, err := r.sql(r.db, `SELECT transport.check_model();
SELECT jsonb_build_object(
 'messages',(SELECT count(*) FROM message_key),
 'message_results',(SELECT jsonb_object_agg(result,n) FROM (SELECT result,count(*) n FROM message_key GROUP BY result) x),
 'message_dates',(SELECT jsonb_object_agg(d,n) FROM (SELECT first_received_at::date d,count(*) n FROM message_key GROUP BY 1) x),
 'device_frequency',(SELECT jsonb_object_agg(device_id,n) FROM (SELECT device_id,count(*) n FROM message_key GROUP BY device_id) x),
 'executions',(SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM execution GROUP BY status) x),
 'duties',(SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) n FROM duty GROUP BY status) x),
 'routes',(SELECT jsonb_object_agg(route_id,n) FROM (SELECT route_id,count(*) n FROM planned_trip GROUP BY route_id) x),
 'pending_checks',(SELECT count(*) FROM transport_private.model_check_queue),
 'database_bytes',pg_database_size(current_database()),
 'table_sizes',(SELECT jsonb_object_agg(relname,pg_total_relation_size(relid)) FROM pg_stat_user_tables WHERE schemaname='transport'));
`)
	if err != nil {
		return err
	}
	var stats any
	if err = json.Unmarshal([]byte(strings.TrimSpace(out)), &stats); err != nil {
		return err
	}
	return r.save("verification-"+m.Mode+".json", map[string]any{"status": "PASS", "row_counts_match_manifest": true, "business_constraints": "PASS", "business_rule_checks": businessChecks, "manifest": m, "passages_have_accepted_messages": true, "facts_not_after_anchor": true, "constraints_and_triggers_enabled": true, "distributions": stats})
}

func (r *runner) businessChecks(db string) (map[string]int, error) {
	out, err := r.file(db, "sql/kt03/check_business_rules.sql")
	if err != nil {
		return nil, err
	}
	var counts map[string]int
	if err = json.Unmarshal([]byte(strings.TrimSpace(out)), &counts); err != nil {
		return nil, err
	}
	if len(counts) != 2 {
		return nil, fmt.Errorf("expected two business rule checks")
	}
	return counts, nil
}
