package kt03

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *runner) testBusinessChecks(db string) error {
	sql, err := os.ReadFile(filepath.Join(r.root, "sql/kt03/check_business_rules.sql"))
	if err != nil {
		return err
	}
	cases := []struct {
		Name, Setup  string
		Early, Order int
	}{
		{Name: "valid fixture"},
		{Name: "reject three-minute early start", Early: 1, Setup: `
SELECT release_duty(1,2,'2026-09-22 09:50+07');
UPDATE execution SET status='running',actual_start='2026-09-22 09:57+07',next_point_no=2 WHERE trip_id=1;
INSERT INTO passage VALUES(1,1,'2026-09-22 09:57+07','demo-device-1','03000000-0000-4000-8000-000000000010','Early start regression');`},
		{Name: "reject telemetry after review", Order: 1, Setup: `
UPDATE duty SET status='on_line',released_at='2026-09-22 09:50+07' WHERE id=1;
UPDATE planned_trip SET frozen_at='2026-09-22 09:50+07' WHERE id IN (1,2);
INSERT INTO execution(trip_id,status,actual_start,actual_end,completion_source,review_due_at)
VALUES(1,'review',NULL,NULL,NULL,'2026-09-22 10:15+07'),
      (2,'done','2026-09-22 11:00+07','2026-09-22 11:20+07','telemetry','2026-09-22 11:35+07');
INSERT INTO passage VALUES
 (2,1,'2026-09-22 11:00+07','demo-device-1','03000000-0000-4000-8000-000000000011','Order regression'),
 (2,2,'2026-09-22 11:10+07','demo-device-1','03000000-0000-4000-8000-000000000012','Order regression'),
 (2,3,'2026-09-22 11:20+07','demo-device-1','03000000-0000-4000-8000-000000000013','Order regression');`},
	}
	results := map[string]map[string]int{}
	for _, c := range cases {
		out, err := r.sql(db, "BEGIN;\n"+c.Setup+"\nSET CONSTRAINTS ALL IMMEDIATE;\n"+string(sql)+"\nROLLBACK;")
		if err != nil {
			return err
		}
		var counts map[string]int
		if err = json.Unmarshal([]byte(strings.TrimSpace(out)), &counts); err != nil {
			return err
		}
		if len(counts) != 2 || counts["telemetry_start_outside_window"] != c.Early || counts["telemetry_after_unresolved_review"] != c.Order {
			return fmt.Errorf("business regression %s: unexpected %v", c.Name, counts)
		}
		results[c.Name] = counts
	}
	return r.save("business-regression.json", map[string]any{"status": "PASS", "cases": results})
}
