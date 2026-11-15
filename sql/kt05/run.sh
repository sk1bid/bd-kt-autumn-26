#!/bin/sh
set -eu
# Run from the repository root. The proxy client mounts this checkout at /work.
docker compose -f deploy/kt04/compose.yaml exec -T proxy-client psql -Xw -v ON_ERROR_STOP=1 \
    -d 'host=localhost port=55405 user=kt04_analyst dbname=transport_kt04 sslmode=verify-full sslrootcert=/run/secrets/ca.crt passfile=/run/secrets/kt04_analyst.pgpass target_session_attrs=standby' \
    -v period_start='2026-09-22 00:00+07' -v period_end='2026-09-24 00:00+07' \
    -v min_late=1 -v limit=10 -v device_id=1 -f /work/sql/kt05/catalog.sql
