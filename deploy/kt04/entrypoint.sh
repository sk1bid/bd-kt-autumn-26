#!/bin/sh
set -eu

mode=$1
runtime=/run/kt04

mkdir -p "$runtime"
install -m 0644 /run/secrets/ca.crt "$runtime/ca.crt"
install -o postgres -g postgres -m 0644 /run/secrets/server.crt "$runtime/server.crt"
install -o postgres -g postgres -m 0600 /run/secrets/server.key "$runtime/server.key"

set -- postgres -D "$PGDATA" -c config_file=/work/deploy/kt04/postgresql.conf
case "$mode" in
    primary|restore)
        install -o postgres -g postgres -m 0600 /run/secrets/postgres.password "$runtime/postgres.password"
        export POSTGRES_PASSWORD_FILE="$runtime/postgres.password"
        export POSTGRES_INITDB_ARGS="--auth-local=peer --auth-host=scram-sha-256"
        if [ "$mode" = primary ]; then
            chown postgres:postgres /wal-archive
            chmod 0700 /wal-archive
            set -- "$@" -c archive_mode=on \
                -c "archive_command=sh /work/deploy/kt04/archive-wal.sh '%p' '%f'"
        fi
        ;;
    replica)
        install -o postgres -g postgres -m 0600 /run/secrets/kt04_replicator.pgpass "$runtime/replication.pgpass"
        if [ ! -s "$PGDATA/PG_VERSION" ]; then
            if [ -e "$PGDATA" ]; then
                echo 'Replica PGDATA exists without PG_VERSION. Inspect its dedicated volume before retrying.' >&2
                exit 1
            fi
            # Keep interrupted copies separate from the final PGDATA directory.
            staged=$(mktemp -d "$(dirname "$PGDATA")/kt04-basebackup.XXXXXX")
            trap 'rm -rf "$staged"' EXIT
            trap 'exit 1' HUP INT TERM
            chown postgres:postgres "$staged"
            gosu postgres pg_basebackup --pgdata="$staged" --write-recovery-conf \
                --wal-method=stream --slot=kt04_replica --checkpoint=fast --no-password \
                --dbname="host=primary port=5432 user=kt04_replicator application_name=kt04_replica connect_timeout=5 sslmode=verify-full sslrootcert=$runtime/ca.crt passfile=$runtime/replication.pgpass"
            mv "$staged" "$PGDATA"
            trap - EXIT HUP INT TERM
        fi
        ;;
    pitr)
        if [ ! -s "$PGDATA/PG_VERSION" ]; then
            echo 'PITR requires a physical backup in the dedicated pitr-data volume.' >&2
            exit 1
        fi
        ;;
    *)
        echo 'Unknown KT-04 PostgreSQL service mode.' >&2
        exit 1
        ;;
esac

exec docker-entrypoint.sh "$@"
