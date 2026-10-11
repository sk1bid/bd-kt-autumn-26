package kt03

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func csvRows(t *testing.T, r *runner, table string) []map[string]string {
	t.Helper()
	m, err := r.readManifest()
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for _, f := range m.Tables {
		if f.Table == table {
			columns = strings.Split(f.Columns, ",")
		}
	}
	f, err := os.Open(filepath.Join(r.data, table+".csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	out := make([]map[string]string, len(rows))
	for i, row := range rows {
		out[i] = map[string]string{}
		for j, col := range columns {
			out[i][col] = row[j]
		}
	}
	return out
}

func TestGeneratedTripsRespectStartAndDutyOrder(t *testing.T) {
	for _, seed := range []int64{1, 42, 20261011} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			r := &runner{ctx: context.Background(), mode: "dev", seed: seed, rows: 20000, data: t.TempDir(), out: t.TempDir()}
			if err := r.generate(); err != nil {
				t.Fatal(err)
			}
			parse := func(s string) time.Time {
				v, e := time.Parse(time.RFC3339, s)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			starts := map[string]time.Time{}
			for _, row := range csvRows(t, r, "planned_trip") {
				starts[row["id"]] = parse(row["starts_at"])
			}
			executions := map[string]map[string]string{}
			for _, row := range csvRows(t, r, "execution") {
				executions[row["trip_id"]] = row
			}
			earlyLaterPoints := 0
			for _, row := range csvRows(t, r, "passage") {
				id := row["trip_id"]
				arrived := parse(row["arrived_at"])
				if row["point_no"] == "1" {
					if arrived.Before(starts[id].Add(-2*time.Minute)) || arrived.After(starts[id].Add(15*time.Minute)) {
						t.Fatalf("trip %s starts outside BP-10 window", id)
					}
					if !arrived.Equal(parse(executions[id]["actual_start"])) {
						t.Fatalf("trip %s start differs from first passage", id)
					}
				} else {
					var point int
					fmt.Sscan(row["point_no"], &point)
					if arrived.Before(starts[id].Add(time.Duration((point-1)*120)*time.Second - 2*time.Minute)) {
						earlyLaterPoints++
					}
				}
			}
			if earlyLaterPoints == 0 {
				t.Fatal("early passages disappeared from generated data")
			}
			duties := map[string][]string{}
			for _, row := range csvRows(t, r, "assignment") {
				duties[row["duty_id"]] = append(duties[row["duty_id"]], row["trip_id"])
			}
			for duty, ids := range duties {
				sort.Slice(ids, func(i, j int) bool { return starts[ids[i]].Before(starts[ids[j]]) })
				unresolved := false
				for _, id := range ids {
					e := executions[id]
					if unresolved && (e["completion_source"] == "telemetry" || e["status"] == "running") {
						t.Fatalf("duty %s trip %s starts after unresolved review", duty, id)
					}
					if e["status"] == "review" {
						unresolved = true
					}
				}
			}
		})
	}
}
