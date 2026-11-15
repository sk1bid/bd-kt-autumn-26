#!/bin/sh
set -eu
dc() { docker compose -p transport-kt04 -f deploy/kt04/compose.yaml "$@"; }
sql() {
    dc exec -T proxy-client psql -XAtw -v ON_ERROR_STOP=1 -d "host=localhost port=$1 user=$2 dbname=transport_kt04 connect_timeout=3 sslmode=verify-full sslrootcert=/run/secrets/ca.crt passfile=/run/secrets/$2.pgpass"
}
rw() { sql 55404 kt04_app; }
ro() { sql 55405 kt04_analyst; }
probe="SELECT session_user,pg_is_in_recovery(),ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()"
test "$(echo "$probe" | rw)" = 'kt04_app|f|t'
test "$(echo "$probe" | ro)" = 'kt04_analyst|t|t'
echo "TLS roles: kt04_app on primary, kt04_analyst on standby"
# Write a real large object owned by the application, then roll it back.
round_trip=$(rw <<'SQL'
BEGIN;
SELECT lo_from_bytea(0, 'proxy-check') AS oid \gset
SELECT lo_get(:oid) = 'proxy-check'::bytea AS round_trip;
ROLLBACK;
SQL
)
printf '%s\n' "$round_trip"
printf '%s\n' "$round_trip" | grep -qx t
if [ "${1:-}" = "--outage" ]; then
    trap 'dc start replica >/dev/null' EXIT
    dc stop replica >/dev/null
    if echo "$probe" | ro; then exit 1; fi
    test "$(echo "$probe" | rw)" = 'kt04_app|f|t'
    dc start replica >/dev/null
    for i in $(seq 1 45); do
        if [ "$(echo "$probe" | ro 2>/dev/null)" = 'kt04_analyst|t|t' ]; then
            trap - EXIT
            echo "Replica offline: report failed, primary available. Replica restored."
            exit 0
        fi
        sleep 1
    done
    exit 1
fi
