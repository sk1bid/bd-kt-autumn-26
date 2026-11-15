#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
actual=$(mktemp)
container=''
cleanup() {
    rm -f "$actual"
    if [ -n "$container" ]; then docker rm -f "$container" >/dev/null; fi
}
trap cleanup EXIT
container=$(docker run -d --network none --tmpfs /var/lib/postgresql/data \
    -e POSTGRES_HOST_AUTH_METHOD=trust -v "$PWD/sql:/sql:ro" \
    postgres:17-alpine@sha256:266d3c257594e195b309ae9d1805e54c788f1d7b375f56bf6c367aac6c917e7b)
# TCP on internal loopback becomes available only after postgres completes initdb.
for i in $(seq 1 60); do
    if docker exec "$container" pg_isready -qh 127.0.0.1; then break; fi
    sleep 1
done
docker exec -i "$container" psql -Xq -U postgres -v ON_ERROR_STOP=1 < tests/kt05/fixture.sql
docker exec -e PGTZ=UTC "$container" psql -XqAt -U postgres -P null=NULL \
    -v ON_ERROR_STOP=1 -v period_start='2026-09-22 00:00+00' -v period_end='2026-09-23 00:00+00' \
    -v min_late=1 -v limit=10 -v device_id=1 -f /sql/kt05/catalog.sql > "$actual"
diff -u tests/kt05/expected.txt "$actual"
echo 'PASS: all 8 reports match the 15 expected rows, including order and NULLs.'
