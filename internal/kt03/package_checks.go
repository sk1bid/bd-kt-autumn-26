package kt03

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

var benchmarkNames = []string{"schedule", "history_hot", "history_rare"}

func requiredPackageFiles() []string {
	paths := []string{"docs/kt03/README.md", "docs/kt03/01-report.md", "docs/kt03/report.tmpl", "go.mod", "compose.yaml", "internal/kt03/tasks.mk", "artifacts/kt03/README.md",
		"sql/kt03/check_business_rules.sql", "sql/kt03/queries/01_schedule_before.sql", "sql/kt03/optimize.sql",
		"sql/kt03/scenarios/commit.sql", "sql/kt03/scenarios/rollback.sql", "sql/kt03/scenarios/locks.sql",
		"artifacts/kt03/environment.json", "artifacts/kt03/pilot.json", "artifacts/kt03/load-comparison.json",
		"artifacts/kt03/regression.json", "artifacts/kt03/business-regression.json", "artifacts/kt03/scenarios/lock-wait.json",
		"artifacts/kt03/scenarios/commit.json", "artifacts/kt03/scenarios/rollback.json", "artifacts/kt03/scenarios/concurrency.json"}
	paths = append(paths, queryPaths()...)
	for _, mode := range []string{"dev", "load"} {
		root := "artifacts/kt03/" + mode + "/"
		paths = append(paths, "artifacts/kt03/manifest-"+mode+".json", root+"verification-"+mode+".json")
		if mode == "dev" {
			continue
		}
		paths = append(paths, root+"bench/verification.json", root+"bench/measurements.json", root+"bench/comparison.csv", root+"queries/parameters.json", root+"queries/verification.json")
		for _, q := range queryFiles {
			paths = append(paths, root+"queries/"+q+".json")
		}
		for _, q := range benchmarkNames {
			for _, stage := range []string{"before", "after"} {
				paths = append(paths, root+"plans/"+q+"-"+stage+"-01.json", root+"bench/"+q+"-"+stage+"-result.json")
			}
		}
	}
	return paths
}

func (r *runner) checkPackage() error {
	for _, path := range requiredPackageFiles() {
		info, err := os.Stat(filepath.Join(r.root, path))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("missing or empty artifact: %s", path)
		}
	}
	base := filepath.Join(r.root, "artifacts/kt03")
	for _, path := range []string{"regression.json", "business-regression.json", "load-comparison.json", "scenarios/commit.json", "scenarios/rollback.json", "scenarios/concurrency.json"} {
		var proof struct {
			Status string `json:"status"`
		}
		if err := readJSON(filepath.Join(base, path), &proof); err != nil {
			return err
		}
		if proof.Status != "PASS" {
			return fmt.Errorf("missing PASS: %s", path)
		}
	}
	for _, mode := range []string{"dev", "load"} {
		var m manifest
		if err := readJSON(filepath.Join(base, "manifest-"+mode+".json"), &m); err != nil {
			return err
		}
		if m.Version != generatorVersion || m.Mode != mode {
			return fmt.Errorf("outdated %s manifest", mode)
		}
		messages := 0
		for _, t := range m.Tables {
			if t.Table == "message_key" {
				messages = t.Rows
			}
		}
		if mode == "dev" && (messages < 50000 || messages > 100000) || mode == "load" && messages < 3000000 {
			return fmt.Errorf("insufficient %s dataset: %d messages", mode, messages)
		}
		var verification struct {
			Status   string         `json:"status"`
			Manifest manifest       `json:"manifest"`
			Checks   map[string]int `json:"business_rule_checks"`
		}
		if err := readJSON(filepath.Join(base, mode, "verification-"+mode+".json"), &verification); err != nil {
			return err
		}
		if verification.Status != "PASS" || !reflect.DeepEqual(m, verification.Manifest) || len(verification.Checks) != 2 {
			return fmt.Errorf("missing or stale %s data verification", mode)
		}
		for _, key := range []string{"telemetry_start_outside_window", "telemetry_after_unresolved_review"} {
			n, ok := verification.Checks[key]
			if !ok || n != 0 {
				return fmt.Errorf("business check %s not passed", key)
			}
		}
		if mode == "dev" {
			continue
		}
		for _, stage := range []string{"queries", "bench"} {
			var proof struct {
				Status   string            `json:"status"`
				Manifest manifest          `json:"manifest"`
				Digests  map[string]string `json:"sql_sha256"`
				Runs     int               `json:"runs"`
			}
			if err := readJSON(filepath.Join(base, mode, stage, "verification.json"), &proof); err != nil {
				return err
			}
			if proof.Status != "PASS" || !reflect.DeepEqual(m, proof.Manifest) {
				return fmt.Errorf("missing or stale %s %s results", mode, stage)
			}
			paths := queryPaths()
			if stage == "bench" {
				paths = append(paths, "sql/kt03/queries/01_schedule_before.sql", "sql/kt03/optimize.sql")
			}
			current, err := fileDigests(r.root, paths)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, proof.Digests) {
				return fmt.Errorf("SQL changed after %s %s run", mode, stage)
			}
			if stage == "bench" {
				if proof.Runs < 2 {
					return fmt.Errorf("at least two measured runs required")
				}
				if err = r.checkPlans(filepath.Join(base, mode), proof.Runs); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *runner) checkPlans(root string, runs int) error {
	var measurements []measurement
	if err := readJSON(filepath.Join(root, "bench/measurements.json"), &measurements); err != nil {
		return err
	}
	if len(measurements) != 6 {
		return fmt.Errorf("expected six benchmark cases")
	}
	seen := map[string]bool{}
	for _, m := range measurements {
		name := m.Query + "-" + m.Stage
		valid := false
		for _, q := range benchmarkNames {
			if m.Query == q {
				valid = true
			}
		}
		if !valid || (m.Stage != "before" && m.Stage != "after") || seen[name] || len(m.Times) != runs || m.Median != median(m.Times) {
			return fmt.Errorf("incomplete or incorrect measurements: %s", name)
		}
		seen[name] = true
		for i, ms := range m.Times {
			path := filepath.Join(root, "plans", fmt.Sprintf("%s-%02d.json", name, i+1))
			var plans []struct {
				Time float64        `json:"Execution Time"`
				Plan map[string]any `json:"Plan"`
			}
			if err := readJSON(path, &plans); err != nil {
				return err
			}
			if len(plans) != 1 || plans[0].Time != ms || plans[0].Plan["Actual Rows"] == nil || plans[0].Plan["Shared Hit Blocks"] == nil {
				return fmt.Errorf("plan and measurement differ: %s", path)
			}
		}
	}
	for _, q := range benchmarkNames {
		before, err := os.ReadFile(filepath.Join(root, "bench", q+"-before-result.json"))
		if err != nil {
			return err
		}
		after, err := os.ReadFile(filepath.Join(root, "bench", q+"-after-result.json"))
		if err != nil {
			return err
		}
		if !bytes.Equal(bytes.TrimSpace(before), bytes.TrimSpace(after)) {
			return fmt.Errorf("optimization changed %s results", q)
		}
	}
	return nil
}
