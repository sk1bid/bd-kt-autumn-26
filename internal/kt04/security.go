package kt04

import (
	"fmt"
	"strings"
)

type securityCheck struct {
	Test   string `json:"test"`
	Role   string `json:"role"`
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (r *Runner) Security() error {
	var checks []securityCheck
	record := func(label, role string, result Result, commandErr error, expected, denial string) error {
		output := strings.TrimSpace(result.Out)
		diagnostic := strings.TrimSpace(result.Err)
		ok := commandErr == nil && result.Code == 0 && output == expected
		if denial != "" {
			ok = commandErr != nil && result.Code != 0 && strings.Contains(diagnostic+"\n"+output, denial)
		}
		if !ok {
			return fmt.Errorf("security check %q (%s): exit=%d, output=%q, stderr=%q, command error=%v",
				label, role, result.Code, output, diagnostic, commandErr)
		}
		checks = append(checks, securityCheck{Test: label, Role: role, OK: ok, Output: output, Error: diagnostic})
		return nil
	}
	test := func(label, role, query, expected, denial string) error {
		result, err := r.SQL(query, "primary", role, DB, "verify-full")
		return record(label, role, result, err, expected, denial)
	}
	const safeRole = `SELECT rolcanlogin AND NOT (rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls)
		FROM pg_roles WHERE rolname = current_user`
	const noMembership = `SELECT NOT EXISTS (
		SELECT 1 FROM pg_roles
		WHERE rolname <> current_user AND pg_has_role(oid, 'MEMBER')
		  AND (current_user <> 'kt04_admin' OR rolname NOT IN (
			'pg_monitor', 'pg_read_all_settings', 'pg_read_all_stats', 'pg_stat_scan_tables'
		  ))
	) AND NOT EXISTS (
		SELECT 1 FROM pg_auth_members
		WHERE member = (SELECT oid FROM pg_roles WHERE rolname = current_user) AND admin_option
	)`
	for _, role := range []string{"kt04_admin", "kt04_app", "kt04_analyst", "kt04_driver1", "kt04_driver2"} {
		if err := test("real LOGIN", role, "SELECT session_user || '/' || current_user", role+"/"+role, ""); err != nil {
			return err
		}
		if err := test("restricted role attributes", role, safeRole, "t", ""); err != nil {
			return err
		}
		if err := test("no membership in other application or privileged roles", role, noMembership, "t", ""); err != nil {
			return err
		}
		if err := test("deny SET ROLE postgres", role, "SET ROLE postgres", "", "42501:"); err != nil {
			return err
		}
	}
	for _, role := range []string{"kt04_admin", "kt04_app", "kt04_analyst"} {
		if err := test("read duties", role, "SELECT count(id) FROM transport.duty", "3", ""); err != nil {
			return err
		}
		if err := test("deny CREATE ROLE", role, "BEGIN; CREATE ROLE forbidden_kt04; ROLLBACK", "", "42501:"); err != nil {
			return err
		}
		if err := test("deny direct duty status update", role, "UPDATE transport.duty SET status='planned' WHERE false", "", "42501:"); err != nil {
			return err
		}
	}
	for _, driver := range []struct{ role, dutyID string }{{"kt04_driver1", "1"}, {"kt04_driver2", "2"}} {
		if err := test("RLS exposes own duty", driver.role, "SELECT id FROM transport.duty ORDER BY id", driver.dutyID, ""); err != nil {
			return err
		}
		if err := test("RLS hides other duties", driver.role, "SELECT count(id) FROM transport.duty WHERE id <> "+driver.dutyID, "0", ""); err != nil {
			return err
		}
		if err := test("RLS DDL denied", driver.role, "BEGIN; ALTER TABLE transport.duty DISABLE ROW LEVEL SECURITY; ROLLBACK", "", "42501:"); err != nil {
			return err
		}
		if err := test("row_security=off cannot bypass policy", driver.role, "SET row_security=off; SELECT id FROM transport.duty", "", "42501:"); err != nil {
			return err
		}
		if err := test("deny owner-backed duty view", driver.role, "SELECT count(*) FROM transport.duty_plan", "", "42501:"); err != nil {
			return err
		}
		if err := test("deny duty update", driver.role, "UPDATE transport.duty SET status='planned' WHERE false", "", "42501:"); err != nil {
			return err
		}
	}
	probes := []struct{ label, role, query, expected, denial string }{
		{"admin can monitor", "kt04_admin", "SELECT pg_has_role('pg_monitor', 'USAGE')", "t", ""},
		{"admin can read audit", "kt04_admin", "SELECT count(*) >= 0 FROM transport_private.duty_status_audit", "t", ""},
		{"admin cannot alter audit", "kt04_admin", "UPDATE transport_private.duty_status_audit SET new_status='forged' WHERE false", "", "42501:"},
		{"analyst can run report", "kt04_analyst", "SELECT planned_trips FROM transport.schedule_report('2026-09-22 00:00+07', '2026-09-23 00:00+07')", "4", ""},
		{"analyst cannot mutate through function", "kt04_analyst", "BEGIN; SELECT transport.release_duty(1,2,'2026-09-22 09:50+07',NULL); ROLLBACK", "", "42501:"},
		{"app can run report", "kt04_app", "SELECT planned_trips FROM transport.schedule_report('2026-09-22 00:00+07', '2026-09-23 00:00+07')", "4", ""},
		{"app cannot edit business roles", "kt04_app", "DELETE FROM transport.user_role WHERE false", "", "42501:"},
		{"app cannot delete audit entries", "kt04_app", "DELETE FROM transport_private.duty_status_audit WHERE false", "", "42501:"},
		{"app cannot create temporary tables", "kt04_app", "CREATE TEMP TABLE forbidden_kt04 (id int)", "", "42501:"},
		{"TLS active", "kt04_app", "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()", "t", ""},
	}
	for _, probe := range probes {
		if err := test(probe.label, probe.role, probe.query, probe.expected, probe.denial); err != nil {
			return err
		}
	}

	result, err := r.SQL("SELECT 1", "primary", "kt04_app", DB, "disable")
	if err := record("non-TLS connection rejected", "kt04_app", result, err, "", "pg_hba.conf"); err != nil {
		return err
	}
	testTLS := func(host, rootCert string) (Result, error) {
		return r.DC("exec", "-T", "-e", "PGHOST="+host, "-e", "PGHOSTADDR=127.0.0.1",
			"-e", "PGSSLMODE=verify-full", "-e", "PGSSLROOTCERT="+rootCert,
			"-e", "PGPASSFILE=/run/secrets/kt04_app.pgpass", "-e", "PGCONNECT_TIMEOUT=3",
			"primary", "psql", "-X", "-w", "-U", "kt04_app", "-d", DB, "-c", "SELECT 1")
	}
	result, err = testTLS("primary", "/run/secrets/kt04-missing-ca.crt")
	if err := record("missing TLS trust root rejected", "kt04_app", result, err, "", "root certificate file"); err != nil {
		return err
	}
	result, err = testTLS("kt04-wrong.invalid", "/run/secrets/ca.crt")
	if err := record("TLS server name mismatch rejected", "kt04_app", result, err, "", "does not match host name"); err != nil {
		return err
	}
	result, err = r.Client("primary", "postgres", "read")
	if err := record("client explicitly rejects superuser", "postgres", result, err, "", "postgresql_role_denied"); err != nil {
		return err
	}
	return r.Save("security", checks)
}
