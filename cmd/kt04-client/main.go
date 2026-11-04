package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	attemptTimeout = 5 * time.Second
	deniedMarker   = "kt04_role_denied"
	commitMarker   = "kt04_release_committed"

	// psql evaluates the guard and runs the operation on the same connection.
	guard = `BEGIN;
SET LOCAL search_path = pg_catalog;
SET LOCAL statement_timeout = '4s';
SET LOCAL lock_timeout = '2s';

WITH RECURSIVE memberships(roleid, admin_option) AS (
    SELECT m.roleid, m.admin_option
    FROM pg_auth_members AS m
    JOIN pg_roles AS login ON login.oid = m.member
    WHERE login.rolname = session_user
    UNION
    SELECT m.roleid, m.admin_option
    FROM pg_auth_members AS m
    JOIN memberships AS reachable ON reachable.roleid = m.member
)
SELECT EXISTS (
    SELECT 1
    FROM pg_roles AS login
    WHERE login.rolname = session_user AND login.rolname = current_user
      AND login.rolname IN ('kt04_app', 'kt04_analyst')
      AND login.rolcanlogin
      AND NOT (login.rolsuper OR login.rolcreatedb OR login.rolcreaterole
               OR login.rolreplication OR login.rolbypassrls)
) AND NOT EXISTS (
    SELECT 1
    FROM memberships AS m
    JOIN pg_roles AS r ON r.oid = m.roleid
    WHERE m.admin_option OR r.rolcanlogin
       OR r.rolname <> CASE session_user WHEN 'kt04_app' THEN 'kt04_runtime' ELSE 'kt04_readonly' END
       OR r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
) AS kt04_allowed
\gset
\if :kt04_allowed
\else
ROLLBACK;
\echo kt04_role_denied
\quit
\endif
`

	readSQL = `SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY d.id), '[]'::jsonb)
FROM (SELECT id, status FROM transport.duty ORDER BY id) AS d;
`

	releaseSQL = `SELECT transport.release_duty(1, 2, '2026-09-22T09:50:00+07:00'::timestamptz, :'request'::uuid);
`
)

var (
	uuidPattern     = regexp.MustCompile(`^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$`)
	namePattern     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	hostPattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)
	sqlStatePattern = regexp.MustCompile(`(?m)^psql:(?:<stdin>|stdin):[0-9]+: (ERROR|FATAL|PANIC):[ \t]+([0-9A-Z]{5}):`)
)

type duty struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type event struct {
	Time           string         `json:"time"`
	Mode           string         `json:"mode"`
	ElapsedMS      int64          `json:"elapsed_ms"`
	State          string         `json:"state"`
	Error          string         `json:"error,omitempty"`
	SQLState       string         `json:"sqlstate,omitempty"`
	Request        string         `json:"request,omitempty"`
	OutcomeUnknown bool           `json:"outcome_unknown,omitempty"`
	NextAction     string         `json:"next_action,omitempty"`
	DutyCount      *int           `json:"duty_count,omitempty"`
	StatusCounts   map[string]int `json:"status_counts,omitempty"`
	Duties         *[]duty        `json:"duties,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: kt04-client read | release --request UUID | watch [--iterations N] [--interval DURATION]")
		return 2
	}

	mode := args[0]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	// Invalid input can contain secrets. Print only fixed messages.
	flags.SetOutput(io.Discard)
	var request string
	iterations := 1
	var interval time.Duration
	switch mode {
	case "read":
	case "release":
		flags.StringVar(&request, "request", "", "operation UUID")
	case "watch":
		flags.IntVar(&iterations, "iterations", 30, "number of read attempts")
		flags.DurationVar(&interval, "interval", time.Second, "delay between attempts")
	default:
		fmt.Fprintln(stderr, "expected read, release, or watch")
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid arguments for selected mode")
		return 2
	}
	if mode == "release" && !uuidPattern.MatchString(request) {
		fmt.Fprintln(stderr, "release requires --request UUID")
		return 2
	}
	if mode == "watch" && (iterations < 1 || interval < 0) {
		fmt.Fprintln(stderr, "watch requires positive iterations and a nonnegative interval")
		return 2
	}

	env, configError := connectionEnv()
	if configError != "" {
		fmt.Fprintln(stderr, configError)
		return 2
	}
	psql, err := exec.LookPath("psql")
	if err != nil {
		fmt.Fprintln(stderr, "psql executable not found")
		return 2
	}

	encoder := json.NewEncoder(stdout)
	successes := 0
	for i := 0; i < iterations; i++ {
		result := attempt(psql, env, mode, request)
		if err := encoder.Encode(result); err != nil {
			return 1
		}
		if result.State == "ok" {
			successes++
		}
		if i+1 < iterations {
			time.Sleep(interval)
		}
	}
	if successes == 0 {
		return 1
	}
	return 0
}

func connectionEnv() ([]string, string) {
	host := os.Getenv("PGHOST")
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	portNumber, err := strconv.Atoi(port)
	if !hostPattern.MatchString(host) || err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, "PGHOST must name one TCP host and PGPORT must be a valid TCP port"
	}

	database, user := os.Getenv("PGDATABASE"), os.Getenv("PGUSER")
	if !namePattern.MatchString(database) || !namePattern.MatchString(user) {
		return nil, "PGDATABASE and PGUSER must be plain names"
	}
	passFile, rootCert := os.Getenv("PGPASSFILE"), os.Getenv("PGSSLROOTCERT")
	if !filepath.IsAbs(passFile) || !filepath.IsAbs(rootCert) {
		return nil, "PGPASSFILE and PGSSLROOTCERT must be absolute file paths"
	}
	if mode := os.Getenv("PGSSLMODE"); mode != "" && mode != "verify-full" {
		return nil, "PGSSLMODE must be verify-full"
	}

	// Supply only the intended libpq settings. In particular PGOPTIONS,
	// PGSERVICE and PGPASSWORD cannot override this connection.
	return []string{
		"LC_ALL=C",
		"PGHOST=" + host,
		"PGPORT=" + port,
		"PGDATABASE=" + database,
		"PGUSER=" + user,
		"PGPASSFILE=" + passFile,
		"PGSSLROOTCERT=" + rootCert,
		"PGSSLMODE=verify-full",
		"PGGSSENCMODE=disable",
		"PGCONNECT_TIMEOUT=3",
		"PGAPPNAME=kt04-client",
	}, ""
}

func attempt(psql string, env []string, mode, request string) event {
	start := time.Now()
	result := event{Time: start.UTC().Format(time.RFC3339Nano), Mode: mode, Request: request}
	ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
	defer cancel()

	query := readSQL
	finish := "COMMIT;\n"
	args := []string{"-X", "--no-password", "--no-align", "--tuples-only", "--quiet",
		"--set=ON_ERROR_STOP=1", "--set=VERBOSITY=verbose", "--file=-"}
	if mode == "release" {
		query = releaseSQL
		finish += "\\echo " + commitMarker + "\n"
		args = append(args, "--set=request="+request)
	}
	cmd := exec.CommandContext(ctx, psql, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(guard + query + finish)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	result.ElapsedMS = time.Since(start).Milliseconds()

	result.State = "error"
	if err != nil {
		result.Error = "postgresql_connection_or_protocol_error"
		ambiguous := true
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "attempt_timeout"
		} else if match := sqlStatePattern.FindSubmatch(stderr.Bytes()); match != nil {
			result.Error = "postgresql_sql_error"
			result.SQLState = string(match[2])
			ambiguous = string(match[1]) != "ERROR" ||
				strings.HasPrefix(result.SQLState, "08") ||
				strings.HasPrefix(result.SQLState, "57P") ||
				result.SQLState == "40003"
		}
		if mode == "release" && ambiguous {
			result.markUnknown()
		}
		return result
	}
	if strings.TrimSpace(string(out)) == deniedMarker {
		result.Error = "postgresql_role_denied"
		return result
	}
	if mode == "release" {
		if strings.TrimSpace(string(out)) != commitMarker {
			result.Error = "invalid_postgresql_result"
			result.markUnknown()
			return result
		}
	} else {
		duties := []duty{}
		if err := json.Unmarshal(out, &duties); err != nil {
			result.Error = "invalid_postgresql_result"
			return result
		}
		count := len(duties)
		result.DutyCount = &count
		result.Duties = &duties
		result.StatusCounts = make(map[string]int)
		for _, d := range duties {
			result.StatusCounts[d.Status]++
		}
	}
	result.State = "ok"
	return result
}

func (result *event) markUnknown() {
	result.OutcomeUnknown = true
	result.NextAction = "after reconnecting, reuse the same request UUID and operation arguments"
}
