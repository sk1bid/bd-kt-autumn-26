BEGIN;
SET LOCAL search_path=transport,public;
SET LOCAL TIME ZONE 'Asia/Novosibirsk';
CREATE FUNCTION pg_temp.deny(label text, statement text) RETURNS text LANGUAGE plpgsql AS $$
DECLARE state text;
BEGIN
 BEGIN
  EXECUTE statement;
 EXCEPTION WHEN OTHERS THEN
  GET STACKED DIAGNOSTICS state = RETURNED_SQLSTATE;
  IF state<>'42501' THEN RAISE EXCEPTION '%: expected 42501, got % %',label,state,SQLERRM; END IF;
  RETURN label||' ['||state||']';
 END;
 RAISE EXCEPTION '%: direct change accepted',label;
END $$;
GRANT EXECUTE ON FUNCTION pg_temp.deny(text,text) TO transport_app, transport_telemetry;
SET LOCAL ROLE transport_app;
SELECT pg_temp.deny('App: duty status', $$UPDATE duty SET status='cancelled',closed_at='2026-09-22 08:00',closed_by=3,close_reason='x' WHERE id=3$$);
SELECT pg_temp.deny('App: decision row', $$INSERT INTO trip_decision VALUES (1,1,2,'2026-09-22 10:40','insufficient','x','review','review',NULL,NULL,NULL,NULL,NULL)$$);
SELECT pg_temp.deny('App: execution', $$UPDATE execution SET status='failed'$$);
SELECT pg_temp.deny('App: plan copy', $$INSERT INTO planned_trip(route_id,starts_at,path_snapshot) VALUES(1,'2026-09-22 15:00','[(0,0),(1,1)]')$$);
SELECT pg_temp.deny('App: plan points', $$UPDATE trip_point SET stop_name_snapshot='x' WHERE trip_id=4$$);
SELECT pg_temp.deny('App: assignment', $$UPDATE assignment SET ended_at='2026-09-22 08:00',ended_by=1,end_reason='x' WHERE trip_id=3$$);
SELECT pg_temp.deny('App: truncate history', $$TRUNCATE trip_decision$$);
SELECT pg_temp.deny('App: delete deviation', $$DELETE FROM deviation$$);
SELECT pg_temp.deny('App: passage', $$INSERT INTO passage VALUES (1,1,'2026-09-22 10:00','demo-device-1',gen_random_uuid(),'x')$$);
SELECT pg_temp.deny('App: correction', $$INSERT INTO passage_correction VALUES (1,1,1,'2026-09-22 10:00',2,false,'x')$$);
SELECT create_trip(1,'2026-09-22 14:00',1) AS created_trip;
SELECT release_duty(1,2,'2026-09-22 09:50');
INSERT INTO vehicle(registration_no,transport_kind,capacity,admitted_until) VALUES('DEMO-BUS-4','bus',50,'2026-12-31');
INSERT INTO device VALUES ('demo-device-4','vendor-b','b-v1',true);
INSERT INTO device_installation(device_id,vehicle_id,started_at,created_by,reason)
 SELECT 'demo-device-4',id,'2026-09-22 08:00',1,'Новый автобус' FROM vehicle WHERE registration_no='DEMO-BUS-4';
SELECT count(*) AS visible_summary_rows FROM trip_summary;
SET LOCAL ROLE transport_telemetry;
SELECT pg_temp.deny('Telemetry: duty', $$UPDATE duty SET closed_by=2 WHERE id=1$$);
SELECT pg_temp.deny('Telemetry: dispatcher operation', $$SELECT close_duty(1,2,'2026-09-22 11:40','x')$$);
UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
INSERT INTO passage VALUES (1,1,'2026-09-22 10:00','demo-device-1','00000000-0000-0000-0000-000000000021','Первая точка');
RESET ROLE;
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT count(*) FROM connection_state c JOIN vehicle v ON v.id=c.vehicle_id WHERE v.registration_no='DEMO-BUS-4')<>1
    OR (SELECT status FROM execution WHERE trip_id=1)<>'running' OR (SELECT count(*) FROM planned_trip)<>5
 THEN RAISE EXCEPTION 'Permitted operations failed'; END IF;
END $$;
ROLLBACK;
