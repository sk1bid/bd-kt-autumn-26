package kt03

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var queryFiles = []string{"01_schedule", "02_late_routes", "03_open_deviations", "04_message_quality", "05_device_history"}

func (r *runner) parameters() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	anchor, err := time.Parse(time.RFC3339, m.Anchor)
	if err != nil {
		return nil, err
	}
	day := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, anchor.Location())
	return map[string]string{"period_start": stamp(day.AddDate(0, 0, 1-m.Days)), "period_end": stamp(day.AddDate(0, 0, 1)), "min_late": "20", "limit": "20", "device_id": "device-001"}, nil
}
func (r *runner) query(name string, p map[string]string) (string, error) {
	b, err := os.ReadFile(filepath.Join(r.root, "sql/kt03/queries", name+".sql"))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	for k, v := range p {
		s = strings.ReplaceAll(s, ":'"+k+"'", lit(v))
	}
	if strings.Contains(s, ":'") {
		return "", fmt.Errorf("unbound parameter in %s", name)
	}
	return s, nil
}
func (r *runner) result(q string) (json.RawMessage, error) {
	out, err := r.sql(r.db, "SELECT coalesce(jsonb_agg(to_jsonb(q)),'[]') FROM ("+q+") q;")
	if err != nil {
		return nil, err
	}
	var value json.RawMessage
	err = json.Unmarshal([]byte(strings.TrimSpace(out)), &value)
	return value, err
}
func (r *runner) queries() error {
	if err := r.invalidate("queries/verification.json"); err != nil {
		return err
	}
	p, err := r.parameters()
	if err != nil {
		return err
	}
	if err = r.save("queries/parameters.json", p); err != nil {
		return err
	}
	for _, name := range queryFiles {
		q, err := r.query(name, p)
		if err != nil {
			return err
		}
		out, err := r.result(q)
		if err != nil {
			return err
		}
		if err = r.save("queries/"+name+".json", out); err != nil {
			return err
		}
	}
	digests, err := fileDigests(r.root, queryPaths())
	if err != nil {
		return err
	}
	var m manifest
	if err = readJSON(filepath.Join(r.data, "manifest.json"), &m); err != nil {
		return err
	}
	return r.save("queries/verification.json", map[string]any{"status": "PASS", "sql_sha256": digests, "manifest": m})
}

type measurement struct {
	Query  string    `json:"query"`
	Stage  string    `json:"stage"`
	Times  []float64 `json:"execution_ms"`
	Median float64   `json:"median_ms"`
}

func median(v []float64) float64 {
	a := append([]float64(nil), v...)
	sort.Float64s(a)
	if len(a)%2 == 1 {
		return a[len(a)/2]
	}
	return (a[len(a)/2-1] + a[len(a)/2]) / 2
}
func (r *runner) bench() error {
	if err := r.invalidate("bench/verification.json"); err != nil {
		return err
	}
	p, err := r.parameters()
	if err != nil {
		return err
	}
	type workload struct{ Name, Before, After, Device string }
	jobs := []workload{{"schedule", "01_schedule_before", "01_schedule", ""}, {"history_hot", "05_device_history", "05_device_history", "device-001"}, {"history_rare", "05_device_history", "05_device_history", "device-100"}}
	b, err := os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		return err
	}
	var m manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	jobs[2].Device = fmt.Sprintf("device-%03d", m.Vehicles)
	if _, err = r.sql(r.db, "DROP INDEX IF EXISTS transport.message_device_received; ANALYZE;"); err != nil {
		return err
	}
	before := map[string]json.RawMessage{}
	var measurements []measurement
	for _, stage := range []string{"before", "after"} {
		if stage == "after" {
			if _, err = r.file(r.db, "sql/kt03/optimize.sql"); err != nil {
				return err
			}
		}
		for _, job := range jobs {
			name := job.Before
			if stage == "after" {
				name = job.After
			}
			p["device_id"] = job.Device
			if job.Device == "" {
				p["device_id"] = "device-001"
			}
			q, err := r.query(name, p)
			if err != nil {
				return err
			}
			result, err := r.result(q)
			if err != nil {
				return err
			}
			if stage == "before" {
				before[job.Name] = result
			} else if !bytes.Equal(before[job.Name], result) {
				return fmt.Errorf("optimization changed result: %s", job.Name)
			}
			if err = r.save("bench/"+job.Name+"-"+stage+"-result.json", result); err != nil {
				return err
			}
			if err = r.save("bench/"+job.Name+"-parameters.json", p); err != nil {
				return err
			}
			for warm := 0; warm < 2; warm++ {
				if _, err = r.result(q); err != nil {
					return err
				}
			}
			measure := measurement{Query: job.Name, Stage: stage}
			for run := 0; run < r.runs; run++ {
				out, err := r.sql(r.db, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+q+";")
				if err != nil {
					return err
				}
				var plans []struct {
					ExecutionMS float64 `json:"Execution Time"`
				}
				if err = json.Unmarshal([]byte(out), &plans); err != nil {
					return err
				}
				if len(plans) != 1 {
					return fmt.Errorf("expected one plan")
				}
				measure.Times = append(measure.Times, plans[0].ExecutionMS)
				if err = r.saveText(fmt.Sprintf("plans/%s-%s-%02d.json", job.Name, stage, run+1), out); err != nil {
					return err
				}
			}
			measure.Median = median(measure.Times)
			measurements = append(measurements, measure)
			fmt.Printf("%s %s median %.3f ms\n", job.Name, stage, measure.Median)
			if err = r.save("bench/measurements.json", measurements); err != nil {
				return err
			}
		}
	}
	var csvData strings.Builder
	w := csv.NewWriter(&csvData)
	w.Write([]string{"query", "before_median_ms", "after_median_ms", "speedup", "runs"})
	for i, job := range jobs {
		a, b := measurements[i].Median, measurements[i+len(jobs)].Median
		w.Write([]string{job.Name, fmt.Sprintf("%.3f", a), fmt.Sprintf("%.3f", b), fmt.Sprintf("%.2f", a/b), fmt.Sprint(r.runs)})
	}
	w.Flush()
	if err = r.saveText("bench/comparison.csv", csvData.String()); err != nil {
		return err
	}
	digests, err := fileDigests(r.root, append(queryPaths(), "sql/kt03/queries/01_schedule_before.sql", "sql/kt03/optimize.sql"))
	if err != nil {
		return err
	}
	return r.save("bench/verification.json", map[string]any{"status": "PASS", "sql_sha256": digests, "results_identical": true, "warmups": 2, "runs": r.runs, "timing": "EXPLAIN ANALYZE Execution Time, milliseconds; warm cache", "manifest": m})
}
