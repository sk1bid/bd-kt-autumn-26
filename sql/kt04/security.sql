-- Apply as the bootstrap superuser after migrations 001–005 and sql/seed.sql.
-- Provision LOGIN passwords separately.
BEGIN;
SET LOCAL search_path = pg_catalog;

CREATE ROLE kt04_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE kt04_admin LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE kt04_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE kt04_analyst LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE kt04_driver1 LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE kt04_driver2 LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;

DO $$
BEGIN
    EXECUTE format('REVOKE CONNECT, TEMPORARY ON DATABASE %I FROM PUBLIC', current_database());
    EXECUTE format(
        'GRANT CONNECT ON DATABASE %I TO kt04_admin, kt04_app, kt04_analyst, kt04_driver1, kt04_driver2',
        current_database()
    );
END $$;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE kt04_owner REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

ALTER SCHEMA transport OWNER TO kt04_owner;
ALTER SCHEMA transport_private OWNER TO kt04_owner;

-- The migration-created SECURITY DEFINER routines run as a restricted owner.
-- Indexes and identity sequences follow their tables.
DO $$
DECLARE
    relation record;
    routine regprocedure;
BEGIN
    FOR relation IN
        SELECT n.nspname, c.relname, CASE c.relkind WHEN 'v' THEN 'VIEW' ELSE 'TABLE' END AS kind
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname IN ('transport', 'transport_private') AND c.relkind IN ('r', 'p', 'v')
    LOOP
        EXECUTE format('ALTER %s %I.%I OWNER TO kt04_owner',
            relation.kind, relation.nspname, relation.relname);
    END LOOP;

    FOR routine IN
        SELECT p.oid::regprocedure
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname IN ('transport', 'transport_private')
    LOOP
        EXECUTE format('ALTER FUNCTION %s OWNER TO kt04_owner', routine);
    END LOOP;
END $$;

SET LOCAL ROLE kt04_owner;

CREATE TABLE transport_private.driver_login (
    login name PRIMARY KEY,
    driver_id bigint NOT NULL UNIQUE REFERENCES transport.driver(id)
);

INSERT INTO transport_private.driver_login (login, driver_id) VALUES
    ('kt04_driver1', (SELECT id FROM transport.driver WHERE license_no = 'DEMO-001')),
    ('kt04_driver2', (SELECT id FROM transport.driver WHERE license_no = 'DEMO-002'));

CREATE FUNCTION transport.session_driver_id() RETURNS bigint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
    SELECT driver_id FROM transport_private.driver_login WHERE login = session_user
$$;

ALTER TABLE transport.duty ENABLE ROW LEVEL SECURITY;
CREATE POLICY driver_duties ON transport.duty
    FOR SELECT TO kt04_driver1, kt04_driver2
    USING (driver_id = transport.session_driver_id());

CREATE POLICY operator_duties ON transport.duty
    FOR SELECT TO kt04_admin, kt04_app, kt04_analyst, transport_app, transport_telemetry
    USING (true);

-- Preserve the original application group's ability to create planned duties.
CREATE POLICY legacy_duty_insert ON transport.duty
    FOR INSERT TO transport_app WITH CHECK (true);

CREATE TABLE transport_private.duty_status_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    actor name NOT NULL,
    duty_id bigint NOT NULL,
    old_status text NOT NULL,
    new_status text NOT NULL
);

CREATE FUNCTION transport_private.audit_duty_status() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
BEGIN
    INSERT INTO transport_private.duty_status_audit (actor, duty_id, old_status, new_status)
    VALUES (session_user, NEW.id, OLD.status, NEW.status);
    RETURN NULL;
END $$;

CREATE TRIGGER audit_duty_status
    AFTER UPDATE OF status ON transport.duty
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION transport_private.audit_duty_status();

GRANT USAGE ON SCHEMA transport, transport_private TO kt04_admin;
GRANT SELECT ON ALL TABLES IN SCHEMA transport, transport_private TO kt04_admin;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA transport, transport_private TO kt04_admin;

GRANT USAGE ON SCHEMA transport TO kt04_app, kt04_analyst, kt04_driver1, kt04_driver2;
GRANT SELECT ON transport.trip_summary, transport.deviation, transport.operation_request TO kt04_app;
GRANT SELECT (id, status) ON transport.duty TO kt04_app;
GRANT EXECUTE ON FUNCTION transport.release_duty(bigint, bigint, timestamptz, uuid) TO kt04_app;
GRANT EXECUTE ON FUNCTION transport.schedule_report(timestamptz, timestamptz, bigint)
    TO kt04_app, kt04_analyst;
GRANT SELECT ON ALL TABLES IN SCHEMA transport TO kt04_analyst;
GRANT SELECT ON transport.duty TO kt04_driver1, kt04_driver2;
GRANT EXECUTE ON FUNCTION transport.session_driver_id() TO kt04_driver1, kt04_driver2;

RESET ROLE;
GRANT pg_monitor TO kt04_admin;
COMMIT;
