package kt03

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"text/template"
)

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (r *runner) report() error {
	values := map[string]any{}
	for key, path := range map[string]string{
		"Environment": "environment.json", "Load": "load/load-load.json", "LoadVerification": "load/verification-load.json",
		"DevVerification": "dev/verification-dev.json", "Comparison": "load-comparison.json", "Pilot": "pilot.json",
	} {
		var value any
		if err := readJSON(filepath.Join(r.out, path), &value); err != nil {
			return err
		}
		values[key] = value
	}
	pilot := values["Pilot"].([]any)
	for key, index := range map[string]int{"PilotBefore": 2, "PilotAfter": 5} {
		matches := regexp.MustCompile(`Time: ([0-9.]+) ms`).FindAllStringSubmatch(pilot[index].(map[string]any)["server_timings"].(string), -1)
		if len(matches) != 3 {
			return fmt.Errorf("unexpected pilot timing format")
		}
		values[key+"Insert"] = matches[1][1]
		values[key+"Commit"] = matches[2][1]
	}
	var m manifest
	if err := readJSON(filepath.Join(r.out, "manifest-load.json"), &m); err != nil {
		return err
	}
	values["Manifest"] = m
	var d manifest
	if err := readJSON(filepath.Join(r.out, "manifest-dev.json"), &d); err != nil {
		return err
	}
	values["DevManifest"] = d
	var measurements []measurement
	if err := readJSON(filepath.Join(r.out, "load/bench/measurements.json"), &measurements); err != nil {
		return err
	}
	type comparison struct {
		Name                   string
		Before, After, Speedup float64
	}
	var comparisons []comparison
	for _, name := range []string{"schedule", "history_hot", "history_rare"} {
		c := comparison{Name: name}
		for _, v := range measurements {
			if v.Query == name {
				if v.Stage == "before" {
					c.Before = v.Median
				} else {
					c.After = v.Median
				}
			}
		}
		if c.After == 0 {
			return fmt.Errorf("missing measurement %s", name)
		}
		c.Speedup = c.Before / c.After
		comparisons = append(comparisons, c)
	}
	values["Bench"] = comparisons
	var benchProof struct {
		Runs int `json:"runs"`
	}
	if err := readJSON(filepath.Join(r.out, "load/bench/verification.json"), &benchProof); err != nil {
		return err
	}
	values["Runs"] = benchProof.Runs
	for i, name := range queryFiles {
		var rows []map[string]any
		if err := readJSON(filepath.Join(r.out, "load/queries", name+".json"), &rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("query example is empty: %s", name)
		}
		b, err := json.MarshalIndent(rows[0], "", "  ")
		if err != nil {
			return err
		}
		values[fmt.Sprintf("Example%d", i+1)] = string(b)
	}
	var params map[string]string
	if err := readJSON(filepath.Join(r.out, "load/queries/parameters.json"), &params); err != nil {
		return err
	}
	values["Parameters"] = params
	for key, name := range map[string]string{"PlanBefore": "schedule-before-01.json", "PlanAfter": "schedule-after-01.json", "HistoryBefore": "history_rare-before-01.json", "HistoryAfter": "history_rare-after-01.json"} {
		var plans []map[string]any
		if err := readJSON(filepath.Join(r.out, "load/plans", name), &plans); err != nil {
			return err
		}
		values[key] = plans[0]["Plan"]
	}
	tpl, err := template.New("report").Funcs(template.FuncMap{
		"whole":   func(v float64) string { return fmt.Sprintf("%.0f", v) },
		"f3":      func(v float64) string { return fmt.Sprintf("%.3f", v) },
		"f2":      func(v float64) string { return fmt.Sprintf("%.2f", v) },
		"seconds": func(v float64) string { return fmt.Sprintf("%.2f", v/1000) },
		"count": func(tables []*tableFile, name string) int {
			for _, t := range tables {
				if t.Table == name {
					return t.Rows
				}
			}
			return 0
		},
	}).ParseFiles(filepath.Join(r.root, "docs/kt03/report.tmpl"))
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err = tpl.ExecuteTemplate(&b, "report.tmpl", values); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.root, "docs/kt03/01-report.md"), b.Bytes(), 0644)
}
