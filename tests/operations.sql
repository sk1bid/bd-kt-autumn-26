BEGIN;
SET LOCAL search_path=transport,public;
SET LOCAL TIME ZONE 'Asia/Novosibirsk';
SAVEPOINT scenario;
UPDATE route_point SET offset_seconds=1260 WHERE route_id=1 AND point_no=3;
SELECT refresh_plan(1,1,'2026-09-22 08:30','Уточнение расписания');
DO $$ BEGIN
 IF (SELECT arrives_at FROM trip_point WHERE trip_id=1 AND point_no=3)<>'2026-09-22 10:21'::timestamptz
    OR (SELECT upper(plan_span) FROM duty WHERE id=1)<>'2026-09-22 11:20'::timestamptz
    OR (SELECT refreshed_by FROM planned_trip WHERE id=1)<>1 THEN RAISE EXCEPTION 'Refresh failed'; END IF;
END $$;
SELECT release_duty(1,2,'2026-09-22 09:50');
UPDATE vehicle SET admitted_until='2026-09-21' WHERE id=3;
DO $$ BEGIN
 BEGIN
  PERFORM replace_duty(1,3,2,'2026-09-22 09:55','Подмена');
  RAISE EXCEPTION 'Expired replacement accepted';
 EXCEPTION WHEN check_violation THEN NULL;
 END;
 IF (SELECT status FROM duty WHERE id=1)<>'on_line' OR (SELECT status FROM duty WHERE id=3)<>'planned'
    OR (SELECT count(*) FROM assignment WHERE duty_id=1 AND ended_at IS NULL)<>2
    OR (SELECT count(*) FROM assignment WHERE duty_id=3)<>0 THEN RAISE EXCEPTION 'Replacement not atomic'; END IF;
END $$;
UPDATE vehicle SET admitted_until='2026-12-31' WHERE id=3;
SELECT replace_duty(1,3,2,'2026-09-22 09:55','Подмена исправным автобусом');
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT status FROM duty WHERE id=1)<>'closed' OR (SELECT status FROM duty WHERE id=3)<>'on_line'
    OR (SELECT count(*) FROM assignment WHERE duty_id=1 AND ended_at IS NOT NULL)<>2
    OR (SELECT count(*) FROM assignment WHERE duty_id=3 AND ended_at IS NULL)<>2
    OR (SELECT count(*) FROM execution)<>2
    OR (SELECT frozen_at FROM planned_trip WHERE id=1)<>'2026-09-22 09:50'::timestamptz THEN RAISE EXCEPTION 'Replacement lost history or execution'; END IF;
END $$;
SET CONSTRAINTS ALL DEFERRED;
ROLLBACK TO scenario;
SELECT release_duty(1,2,'2026-09-22 09:50');
SELECT id FROM duty WHERE id=1 FOR UPDATE;
UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
INSERT INTO passage VALUES
 (1,1,'2026-09-22 10:00','demo-device-1','00000000-0000-0000-0000-000000000011','Первая точка'),
 (1,2,'2026-09-22 10:10','demo-device-1','00000000-0000-0000-0000-000000000012','Вторая точка'),
 (1,3,'2026-09-22 10:20','demo-device-1','00000000-0000-0000-0000-000000000013','Последняя точка');
UPDATE execution SET status='done',actual_end='2026-09-22 10:20',completion_source='telemetry',next_point_no=NULL WHERE trip_id=1;
SELECT replace_duty(1,3,2,'2026-09-22 10:25','Замена автобуса после первого рейса');
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT duty_id FROM assignment WHERE trip_id=1 AND ended_at IS NULL)<>1
    OR (SELECT duty_id FROM assignment WHERE trip_id=2 AND ended_at IS NULL)<>3
    OR (SELECT started_at FROM assignment WHERE trip_id=2 AND ended_at IS NULL)<>'2026-09-22 10:25'::timestamptz
    OR (SELECT closed_at FROM duty WHERE id=1)<>'2026-09-22 10:25'::timestamptz
    OR (SELECT status FROM execution WHERE trip_id=2)<>'waiting' OR (SELECT status FROM duty WHERE id=3)<>'on_line'
 THEN RAISE EXCEPTION 'KT-01 7.1 replacement example failed'; END IF;
END $$;
SET CONSTRAINTS ALL DEFERRED;
ROLLBACK TO scenario;
SELECT cancel_duty(2,2,'2026-09-22 08:00','Резерв не потребовался');
SELECT assign_trip(3,3,1,'2026-09-22 08:10','Повторное назначение после отмены');
SELECT release_duty(3,2,'2026-09-22 11:50');
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT status FROM duty WHERE id=2)<>'cancelled' OR (SELECT plan_span FROM duty WHERE id=2) IS NOT NULL
    OR (SELECT count(*) FROM assignment WHERE duty_id=2 AND ended_at='2026-09-22 08:00'::timestamptz AND ended_by=2)<>1
    OR (SELECT duty_id FROM assignment WHERE trip_id=3 AND ended_at IS NULL)<>3
    OR NOT EXISTS (SELECT 1 FROM execution WHERE trip_id=3) THEN RAISE EXCEPTION 'KP-05.6 cancellation failed'; END IF;
END $$;
SET CONSTRAINTS ALL DEFERRED;
ROLLBACK TO scenario;
DO $$ DECLARE first bigint; second bigint; BEGIN
 first := create_trip(1,'2026-09-22 14:00',1);
 second := create_trip(1,'2026-09-22 14:00',1);
 IF first<>second OR (SELECT count(*) FROM planned_trip)<>5
    OR (SELECT count(*) FROM trip_point t JOIN route_point r ON r.route_id=1 AND r.point_no=t.point_no JOIN stop s ON s.id=r.stop_id
        WHERE t.trip_id=first AND t.stop_id=s.id AND t.stop_name_snapshot=s.name AND t.arrives_at='2026-09-22 14:00'::timestamptz+make_interval(secs=>r.offset_seconds))<>3
    OR (SELECT path_snapshot::text FROM planned_trip WHERE id=first)<>(SELECT path::text FROM route WHERE id=1)
 THEN RAISE EXCEPTION 'Trip copy is not idempotent or not a copy'; END IF;
END $$;
ROLLBACK TO scenario;
SELECT release_duty(1,2,'2026-09-22 09:50','00000000-0000-0000-0000-0000000000a1');
SELECT release_duty(1,2,'2026-09-22 09:50','00000000-0000-0000-0000-0000000000a1');
DO $$ BEGIN
 IF (SELECT count(*) FROM operation_request)<>1 OR (SELECT status FROM duty WHERE id=1)<>'on_line' OR (SELECT count(*) FROM execution)<>2
 THEN RAISE EXCEPTION 'Repeated request was applied twice'; END IF;
END $$;
ROLLBACK TO scenario;
SELECT release_duty(1,2,'2026-09-22 09:50');
UPDATE execution SET status='review' WHERE trip_id IN (1,2);
SELECT decide_trip(1,2,'2026-09-22 10:30','complete','Подтверждено водителем','2026-09-22 10:00','2026-09-22 10:20');
SELECT decide_trip(2,2,'2026-09-22 11:30','complete','Подтверждено водителем','2026-09-22 11:00','2026-09-22 11:20');
SELECT close_duty(1,2,'2026-09-22 11:30','Смена завершена');
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT status FROM duty WHERE id=1)<>'closed' OR (SELECT unchecked_results FROM duty WHERE id=1) THEN RAISE EXCEPTION 'Valid closure failed'; END IF;
 IF (SELECT row(planned_trips,completed,completed_by_dispatcher,n)::text FROM schedule_report('2026-09-22 00:00','2026-09-23 00:00',1))<>'(4,2,2,0)'
    OR (SELECT row(planned_trips,percent_on_time)::text FROM schedule_report('2026-09-22 00:00','2026-09-23 00:00',999))<>'(0,)'
 THEN RAISE EXCEPTION 'Report route filter failed'; END IF;
END $$;
SET CONSTRAINTS ALL DEFERRED;
ROLLBACK TO scenario;
SELECT unassign_trip(2,1,'2026-09-21 13:00','Перенос');
UPDATE duty SET vehicle_id=1 WHERE id=3;
SELECT assign_trip(2,3,1,'2026-09-21 13:00','Перенос');
UPDATE route_point SET offset_seconds=3600 WHERE route_id=1 AND point_no=3;
SELECT refresh_plan(1,1,'2026-09-22 08:00','Окончание касается следующего наряда');
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF (SELECT upper(plan_span) FROM duty WHERE id=1)<>(SELECT lower(plan_span) FROM duty WHERE id=3) THEN RAISE EXCEPTION 'Touching intervals rejected'; END IF;
END $$;
ROLLBACK;
