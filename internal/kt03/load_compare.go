package kt03

import (
	"fmt"
	"strings"
)

func (r *runner) messageDigest() (string, error) {
	return r.sql(r.db, `SELECT count(*)||'/'||md5(string_agg(md5(to_jsonb(k)::text),'' ORDER BY device_id,message_id)) FROM message_key k;`)
}
func (r *runner) loadCompare() error {
	if err := r.invalidate("load-comparison.json"); err != nil {
		return err
	}
	m, err := r.readManifest()
	if err != nil {
		return err
	}
	var messages int
	for _, t := range m.Tables {
		if t.Table == "message_key" {
			messages = t.Rows
		}
	}
	if messages > 100000 {
		return fmt.Errorf("use the development dataset (at most 100000 messages) for load comparison")
	}
	oldDB, oldMethod := r.db, r.method
	r.db = "transport_kt03_load_compare"
	r.method = "copy"
	defer func() { r.sql("postgres", "DROP DATABASE IF EXISTS "+r.db+";"); r.db, r.method = oldDB, oldMethod }()
	if err = r.reset(r.db, 5); err != nil {
		return err
	}
	if _, err = r.loadStream(m, false); err != nil {
		return err
	}
	expected, err := r.messageDigest()
	if err != nil {
		return err
	}
	timings := map[string][]float64{"copy": {}, "insert": {}}
	for run := 0; run <= r.runs; run++ {
		methods := []string{"copy", "insert"}
		if run%2 == 1 {
			methods = []string{"insert", "copy"}
		}
		for _, method := range methods {
			if _, err = r.sql(r.db, "TRUNCATE transport.message_key;"); err != nil {
				return err
			}
			r.method = method
			elapsed, err := r.loadStream(m, true)
			if err != nil {
				return err
			}
			got, err := r.messageDigest()
			if err != nil {
				return err
			}
			if got != expected {
				return fmt.Errorf("load strategy changed data")
			}
			if run > 0 {
				timings[method] = append(timings[method], elapsed)
			}
			fmt.Printf("load %s run=%d %.1f ms\n", method, run, elapsed)
		}
	}
	medians := map[string]float64{}
	for method, times := range timings {
		medians[method] = median(times)
	}
	return r.save("load-comparison.json", map[string]any{"status": "PASS", "rows": messages, "seed": m.Seed, "warmups_per_method": 1, "runs_per_method": r.runs, "batch_rows_insert": 1000, "transactions_per_run": 1, "wall_ms_including_encoding_transfer_commit": timings, "median_ms": medians, "identical_rows_digest": strings.TrimSpace(expected), "constraints_enabled": true, "indexes": "schema migrations 001–005; optimization index absent"})
}
