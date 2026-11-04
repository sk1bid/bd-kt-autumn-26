package kt04

import (
	"fmt"
	"strings"
)

// Analytics runs the same read-only report on both nodes after replay catches up.
func (r *Runner) Analytics() error {
	l, e := r.SQL("SELECT pg_current_wal_lsn()", "primary", "postgres", DB, "")
	if e != nil {
		return e
	}
	if e = r.Replay(strings.TrimSpace(l.Out)); e != nil {
		return e
	}
	const query = "SELECT row_to_json(t) FROM transport.schedule_report('2026-09-22 00:00+07','2026-09-23 00:00+07') t ORDER BY row_to_json(t)::text"
	primary, e := r.SQL(query, "primary", "kt04_analyst", DB, "")
	if e != nil {
		return e
	}
	replica, e := r.SQL(query, "replica", "kt04_analyst", DB, "")
	if e != nil {
		return e
	}
	if primary.Out != replica.Out || strings.TrimSpace(replica.Out) == "" {
		return fmt.Errorf("analytical reports differ or are empty")
	}
	node, e := r.SQL("SELECT session_user,pg_is_in_recovery()", "replica", "kt04_analyst", DB, "")
	if e != nil {
		return e
	}
	if strings.TrimSpace(node.Out) != "kt04_analyst|t" {
		return fmt.Errorf("analytics not executed by analyst on standby")
	}
	return r.Save("analytics", map[string]any{"role_node": strings.TrimSpace(node.Out), "query": query, "primary": primary.Out, "replica": replica.Out, "equal_after_replay": true})
}
