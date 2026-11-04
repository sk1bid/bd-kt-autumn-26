-- Start psql as kt04_admin with TCP parameters and a PGPASSFILE for all five roles.
-- Each reconnect authenticates directly. Run against the seeded primary.
\set ON_ERROR_STOP on
\pset pager off

SELECT session_user, current_user;
DO $$
BEGIN
    IF session_user <> 'kt04_admin' OR current_user <> session_user
       OR NOT pg_has_role(session_user, 'pg_monitor', 'USAGE')
       OR (SELECT count(*) FROM transport.duty WHERE number IN ('Н-001', 'Н-002', 'Н-003')) <> 3 THEN
        RAISE EXCEPTION 'Admin identity, diagnostics or backup access is wrong';
    END IF;
    PERFORM count(*) FROM transport_private.duty_status_audit;
    PERFORM count(*) FROM pg_stat_replication;
    BEGIN
        CREATE ROLE kt04_test_forbidden;
        RAISE EXCEPTION 'Admin created a role';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Admin CREATE ROLE rejected [42501]';
    END;
    BEGIN
        UPDATE transport_private.duty_status_audit SET new_status = 'forged' WHERE false;
        RAISE EXCEPTION 'Admin can modify audit';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Admin audit write rejected [42501]';
    END;
    IF EXISTS (
        SELECT FROM pg_roles
        WHERE rolname IN ('kt04_admin', 'kt04_app', 'kt04_analyst', 'kt04_driver1', 'kt04_driver2')
          AND (NOT rolcanlogin OR rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'Unsafe attributes on a KT04 login';
    END IF;
END $$;

\connect - kt04_app
SELECT session_user, current_user;
DO $$
BEGIN
    IF session_user <> 'kt04_app' OR current_user <> session_user THEN
        RAISE EXCEPTION 'Wrong application identity';
    END IF;
    PERFORM * FROM transport.schedule_report('2026-09-22 00:00+07', '2026-09-23 00:00+07');
    PERFORM request_id FROM transport.operation_request LIMIT 1;
    PERFORM id, status FROM transport.duty LIMIT 1;
    IF NOT has_function_privilege('transport.release_duty(bigint,bigint,timestamptz,uuid)', 'EXECUTE')
       OR pg_has_role(session_user, 'kt04_owner', 'MEMBER')
       OR pg_has_role(session_user, 'transport_app', 'MEMBER')
       OR pg_has_role(session_user, 'kt04_admin', 'MEMBER') THEN
        RAISE EXCEPTION 'Unexpected application role privileges';
    END IF;
    BEGIN
        UPDATE transport.duty SET status = 'closed' WHERE false;
        RAISE EXCEPTION 'Application can change a duty directly';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Application direct duty update rejected [42501]';
    END;
    BEGIN
        DELETE FROM transport_private.duty_status_audit WHERE false;
        RAISE EXCEPTION 'Application can delete audit entries';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Application audit delete rejected [42501]';
    END;
    BEGIN
        CREATE TEMP TABLE kt04_test_forbidden (id integer);
        RAISE EXCEPTION 'Application can create a temporary table';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Application CREATE TEMP TABLE rejected [42501]';
    END;
END $$;

\connect - kt04_analyst
SELECT session_user, current_user;
DO $$
BEGIN
    IF session_user <> 'kt04_analyst' OR current_user <> session_user THEN
        RAISE EXCEPTION 'Wrong analyst identity';
    END IF;
    PERFORM * FROM transport.schedule_report('2026-09-22 00:00+07', '2026-09-23 00:00+07');
    PERFORM count(*) FROM transport.duty_plan;
    BEGIN
        PERFORM transport.release_duty(1, 2, '2026-09-22 09:50+07', NULL);
        RAISE EXCEPTION 'Analyst can release duties';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Analyst release_duty rejected [42501]';
    END;
    BEGIN
        UPDATE transport.duty SET status = 'closed' WHERE false;
        RAISE EXCEPTION 'Analyst can update duties';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Analyst direct duty update rejected [42501]';
    END;
END $$;

\connect - kt04_driver1
SELECT session_user, current_user, number FROM transport.duty ORDER BY number;
DO $$
BEGIN
    IF session_user <> 'kt04_driver1' OR current_user <> session_user
       OR (SELECT array_agg(number ORDER BY number) FROM transport.duty) IS DISTINCT FROM ARRAY['Н-001'] THEN
        RAISE EXCEPTION 'Driver 1 must see only duty Н-001';
    END IF;
    BEGIN
        PERFORM count(*) FROM transport.duty_plan;
        RAISE EXCEPTION 'Driver 1 can bypass RLS via duty_plan';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Driver 1 duty_plan access rejected [42501]';
    END;
    BEGIN
        PERFORM count(*) FROM transport_private.driver_login;
        RAISE EXCEPTION 'Driver 1 can see the private driver map';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Driver 1 private mapping access rejected [42501]';
    END;
    BEGIN
        SET LOCAL row_security = off;
        PERFORM count(*) FROM transport.duty;
        RAISE EXCEPTION 'Driver 1 can turn off RLS';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Driver 1 cannot bypass RLS with row_security=off [42501]';
    END;
END $$;

\connect - kt04_driver2
SELECT session_user, current_user, number FROM transport.duty ORDER BY number;
DO $$
BEGIN
    IF session_user <> 'kt04_driver2' OR current_user <> session_user
       OR (SELECT array_agg(number ORDER BY number) FROM transport.duty) IS DISTINCT FROM ARRAY['Н-002'] THEN
        RAISE EXCEPTION 'Driver 2 must see only duty Н-002';
    END IF;
    BEGIN
        UPDATE transport.duty SET status = 'closed' WHERE number = 'Н-002';
        RAISE EXCEPTION 'Driver 2 can change duty status';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Driver 2 direct duty update rejected [42501]';
    END;
    BEGIN
        PERFORM count(*) FROM transport.duty_plan;
        RAISE EXCEPTION 'Driver 2 can bypass RLS via duty_plan';
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Driver 2 duty_plan access rejected [42501]';
    END;
END $$;
