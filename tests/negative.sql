BEGIN;
SET LOCAL search_path=transport,public;
SET LOCAL TIME ZONE 'Asia/Novosibirsk';
CREATE FUNCTION pg_temp.reject(label text, statement text, expected text, fragment text) RETURNS text LANGUAGE plpgsql AS $$
DECLARE state text; message text;
BEGIN
 BEGIN
  SET CONSTRAINTS ALL DEFERRED;
  EXECUTE statement;
  SET CONSTRAINTS ALL IMMEDIATE;
 EXCEPTION WHEN OTHERS THEN
  GET STACKED DIAGNOSTICS state = RETURNED_SQLSTATE, message = MESSAGE_TEXT;
  IF state<>expected OR position(fragment IN message)=0 THEN
   RAISE EXCEPTION '%: expected % "%", got %: %',label,expected,fragment,state,message;
  END IF;
  RETURN label||' ['||state||']';
 END;
 RAISE EXCEPTION '%: invalid data accepted',label;
END $$;
CREATE TEMP TABLE prelude(sql text) ON COMMIT DROP;
INSERT INTO prelude VALUES ($$SELECT release_duty(1,2,'2026-09-22 09:50')$$);
SELECT pg_temp.reject('NOT NULL', $$INSERT INTO vehicle(registration_no,transport_kind,capacity,admitted_until) VALUES(NULL,'bus',10,'2026-12-31')$$,'23502','registration_no');
SELECT pg_temp.reject('UNIQUE registration', $$INSERT INTO vehicle(registration_no,transport_kind,capacity,admitted_until) VALUES('DEMO-BUS-1','bus',10,'2026-12-31')$$,'23505','vehicle_registration_no_key');
SELECT pg_temp.reject('CHECK capacity', $$UPDATE vehicle SET capacity=0 WHERE id=1$$,'23514','vehicle_capacity_check');
SELECT pg_temp.reject('CHECK coordinate', $$UPDATE stop SET latitude=91 WHERE id=1$$,'23514','stop_latitude_check');
SELECT pg_temp.reject('FOREIGN KEY', $$INSERT INTO route_point VALUES(1,4,99999,1800)$$,'23503','route_point_stop_id_fkey');
SELECT pg_temp.reject('PRIMARY KEY point', $$INSERT INTO route_point VALUES(1,1,1,0)$$,'23505','route_point_pkey');
SELECT pg_temp.reject('Minimum two points', $$DELETE FROM route_point WHERE route_id=1 AND point_no IN (2,3)$$,'23514','Route requires');
SELECT pg_temp.reject('Point sequence has no gaps', $$DELETE FROM route_point WHERE route_id=1 AND point_no=2$$,'23514','Route requires');
SELECT pg_temp.reject('Increasing offsets', $$UPDATE route_point SET offset_seconds=0 WHERE point_no=2$$,'23514','Route requires');
SELECT pg_temp.reject('User must have role', $$INSERT INTO app_user(login,full_name) VALUES('norole','No role')$$,'23514','at least one role');
SELECT pg_temp.reject('EXCLUDE installation', $$INSERT INTO device_installation(device_id,vehicle_id,started_at,created_by,reason) VALUES('demo-device-1',2,'2026-09-22 09:00',1,'conflict')$$,'23P01','device_installation_device_id');
SELECT pg_temp.reject('EXCLUDE assignment history', $$INSERT INTO assignment(trip_id,duty_id,started_at,ended_at,created_by,reason,ended_by,end_reason) VALUES(1,2,'2026-09-21 13:00','2026-09-21 14:00',1,'conflict',1,'end')$$,'23P01','assignment_trip_id');
SELECT pg_temp.reject('EXCLUDE planned vehicle', $$UPDATE duty SET vehicle_id=1 WHERE id=3; INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES(4,3,'2026-09-21 12:00',1,'extra'); UPDATE trip_point SET arrives_at='2026-09-22 13:10' WHERE trip_id=2 AND point_no=3$$,'23P01','duty_vehicle_id_plan_span_excl');
SELECT pg_temp.reject('Derived interval cannot be forged', $$UPDATE duty SET plan_span='[2026-09-22 15:00,2026-09-22 16:00)' WHERE id=1$$,'23514','Duty interval');
SELECT pg_temp.reject('No empty release', $$SELECT release_duty(3,2,'2026-09-22 09:00')$$,'23514','empty duty');
SELECT pg_temp.reject('Dispatcher role required', $$SELECT release_duty(1,3,'2026-09-22 09:00')$$,'42501','Active dispatcher required');
SELECT pg_temp.reject('Expired admission', $$UPDATE driver SET admitted_until='2026-09-21' WHERE id=1; SELECT release_duty(1,2,'2026-09-22 09:00')$$,'23514','not admitted');
SELECT pg_temp.reject('Frozen plan', $$SELECT release_duty(1,2,'2026-09-22 09:00'); UPDATE trip_point SET stop_name_snapshot='changed' WHERE trip_id=1 AND point_no=1$$,'23514','Frozen trip points');
SELECT pg_temp.reject('Waiting execution blocks close', $$SELECT release_duty(1,2,'2026-09-22 09:00'); SELECT close_duty(1,2,'2026-09-22 09:10','unfinished')$$,'23514','Unfinished trips');
SELECT pg_temp.reject('One execution per trip', $$SELECT release_duty(1,2,'2026-09-22 09:00'); INSERT INTO execution(trip_id,review_due_at) VALUES(1,'2026-09-22 10:15')$$,'23505','execution_pkey');
SELECT pg_temp.reject('Device hash length', $$INSERT INTO message_key(device_id,message_id,content_sha256,first_received_at,first_receipt_id,result) VALUES('demo-device-1','00000000-0000-0000-0000-000000000001','\x01','2026-09-22 10:00','00000000-0000-0000-0000-000000000002','invalid')$$,'23514','message_key_content_sha256_check');
SELECT pg_temp.reject('Closure before trip end', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id IN (1,2);
 SELECT decide_trip(1,2,'2026-09-22 10:30','complete','ok','2026-09-22 10:00','2026-09-22 10:20');
 SELECT decide_trip(2,2,'2026-09-22 11:30','complete','ok','2026-09-22 11:00','2026-09-22 11:20');
 SELECT close_duty(1,2,'2026-09-22 09:55','Проверка')$$,'23514','Closure time precedes trip results');
SELECT pg_temp.reject('Closure before decision', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id IN (1,2);
 SELECT decide_trip(1,2,'2026-09-22 10:30','insufficient','Нет ответа');
 SELECT close_duty(1,2,'2026-09-22 10:00','Проверка',true)$$,'23514','Closure time precedes trip results');
SELECT pg_temp.reject('Closure before release', (SELECT sql FROM prelude)||$$; SELECT close_duty(1,2,'2026-09-22 09:40','Проверка')$$,'23514','Closure cannot precede release');
SELECT pg_temp.reject('Decision before release', $$SELECT release_duty(2,2,'2026-09-22 11:50'); SELECT decide_trip(3,2,'2026-09-22 08:00','fail','Раньше выпуска')$$,'23514','Decision cannot precede release');
SELECT pg_temp.reject('Confirmed end after closure', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id IN (1,2);
 SELECT close_duty(1,2,'2026-09-22 11:40','Непроверенные',true);
 SELECT decide_trip(1,2,'2026-09-22 11:45','complete','ok','2026-09-22 10:00','2026-09-22 11:42')$$,'23514','Confirmed times cannot follow duty closure');
SELECT pg_temp.reject('Start while another trip needs review', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 UPDATE execution SET status='running',actual_start='2026-09-22 11:00',next_point_no=2 WHERE trip_id=2$$,'23514','Another running or review trip');
SELECT pg_temp.reject('Dispatcher completion without decision', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 UPDATE execution SET status='done',actual_start='2026-09-22 10:00',actual_end='2026-09-22 10:20',completion_source='dispatcher',next_point_no=NULL WHERE trip_id=1$$,'23514','does not match dispatcher decisions');
SELECT pg_temp.reject('Failure without decision', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='failed' WHERE trip_id=1$$,'23514','does not match dispatcher decisions');
SELECT pg_temp.reject('Forged decision row', (SELECT sql FROM prelude)||$$; INSERT INTO trip_decision VALUES (1,1,2,'2026-09-22 10:40','complete','подделка','review','done','2026-09-22 10:00','2026-09-22 10:20',NULL,NULL,NULL)$$,'23514','does not match dispatcher decisions');
SELECT pg_temp.reject('Telemetry completion without last point', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
 UPDATE execution SET status='done',actual_end='2026-09-22 10:20',completion_source='telemetry',next_point_no=NULL WHERE trip_id=1$$,'23514','passage of the last point');
SELECT pg_temp.reject('Passage before start', (SELECT sql FROM prelude)||$$; INSERT INTO passage VALUES (1,1,'2026-09-22 10:00','demo-device-1',gen_random_uuid(),'x')$$,'23514','Passage does not match');
SELECT pg_temp.reject('Passage from another bus device', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
 INSERT INTO passage VALUES (1,1,'2026-09-22 10:00','demo-device-2',gen_random_uuid(),'x')$$,'23514','Passage does not match');
SELECT pg_temp.reject('Unreliable passage cannot be restored', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
 INSERT INTO passage VALUES (1,2,'2026-09-22 10:16','demo-device-1',gen_random_uuid(),'x');
 INSERT INTO deviation(duty_id,trip_id,point_no,kind,detected_at,measured_value) VALUES(1,1,2,'late','2026-09-22 10:16',6);
 SELECT close_deviation((SELECT id FROM deviation WHERE trip_id=1),2,'2026-09-22 10:25','unreliable_data','Ошибка координаты');
 INSERT INTO passage_correction VALUES (1,2,2,'2026-09-22 10:26',2,true,'Возврат')$$,'23514','cannot be restored');
SELECT pg_temp.reject('Connection loss belongs to duty only', (SELECT sql FROM prelude)||$$; INSERT INTO deviation(duty_id,trip_id,kind,detected_at,episode_started_at) VALUES(1,1,'connection_lost','2026-09-22 10:05','2026-09-22 10:02')$$,'23514','deviation_connection_lost_duty_only');
SELECT pg_temp.reject('Deleting closed deviation', $$SELECT release_duty(2,2,'2026-09-22 11:50'); SELECT decide_trip(3,2,'2026-09-22 12:01','fail','Поломка');
 SELECT close_deviation((SELECT id FROM deviation WHERE trip_id=3),2,'2026-09-22 12:05','explained','Подтверждено'); DELETE FROM deviation WHERE trip_id=3$$,'23514','append-only');
SELECT pg_temp.reject('Truncating decisions', $$TRUNCATE trip_decision$$,'23514','cannot be truncated');
SELECT pg_temp.reject('Release before plan refresh', $$SELECT refresh_plan(1,1,'2026-09-22 09:59','Позднее уточнение'); SELECT release_duty(1,2,'2026-09-22 09:50')$$,'23514','Release cannot precede plan refresh');
SELECT pg_temp.reject('Continue after closure (KP-05.9)', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id IN (1,2);
 SELECT close_duty(1,2,'2026-09-22 11:40','Непроверенные',true);
 SELECT decide_trip(1,2,'2026-09-22 11:45','continue','Водитель','2026-09-22 10:00',NULL,NULL,2)$$,'23514','Cannot continue');
SELECT pg_temp.reject('Completion ends after decision (KP-05.11)', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,2,'2026-09-22 10:30','complete','ok','2026-09-22 10:00','2026-09-22 10:40')$$,'23514','trip_decision_check1');
SELECT pg_temp.reject('Completion ends before start (KP-05.11)', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,2,'2026-09-22 10:30','complete','ok','2026-09-22 10:20','2026-09-22 10:10')$$,'23514','trip_decision_check1');
SELECT pg_temp.reject('Decision without reason (KP-05.11)', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,2,'2026-09-22 10:30','complete',' ','2026-09-22 10:00','2026-09-22 10:20')$$,'23514','trip_decision_reason_check');
SELECT pg_temp.reject('Decision by analyst', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,3,'2026-09-22 10:30','insufficient','Нет данных')$$,'42501','Active dispatcher required');
SELECT pg_temp.reject('Decision contradicts reliable passage', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='running',actual_start='2026-09-22 10:00',next_point_no=2 WHERE trip_id=1;
 INSERT INTO passage VALUES (1,3,'2026-09-22 10:25','demo-device-1',gen_random_uuid(),'x');
 UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,2,'2026-09-22 10:30','complete','ok','2026-09-22 10:00','2026-09-22 10:20')$$,'23514','contradicts reliable passages');
SELECT pg_temp.reject('Decision time moves backwards', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id=1;
 SELECT decide_trip(1,2,'2026-09-22 10:30','insufficient','Нет ответа');
 SELECT decide_trip(1,2,'2026-09-22 10:29','insufficient','Нет ответа')$$,'23514','cannot move backwards');
SELECT pg_temp.reject('Refresh after release', (SELECT sql FROM prelude)||$$; SELECT refresh_plan(1,1,'2026-09-22 10:00','Поздно')$$,'23514','Plan already released');
SELECT pg_temp.reject('Release of closed duty', (SELECT sql FROM prelude)||$$; UPDATE execution SET status='review' WHERE trip_id IN (1,2);
 SELECT close_duty(1,2,'2026-09-22 11:40','Непроверенные',true); SELECT release_duty(1,2,'2026-09-22 11:41')$$,'23514','Only planned duty');
SELECT pg_temp.reject('Release of cancelled duty', $$SELECT cancel_duty(2,2,'2026-09-22 08:00','Резерв'); SELECT release_duty(2,2,'2026-09-22 11:50')$$,'23514','Only planned duty');
SELECT pg_temp.reject('Cancellation without reason', $$SELECT cancel_duty(3,2,'2026-09-22 08:00',' ')$$,'23514','cancelled with a reason');
SELECT pg_temp.reject('Deviation closed twice', $$SELECT release_duty(2,2,'2026-09-22 11:50'); SELECT decide_trip(3,2,'2026-09-22 12:01','fail','Поломка');
 SELECT close_deviation((SELECT id FROM deviation WHERE trip_id=3),2,'2026-09-22 12:05','explained','Подтверждено');
 SELECT close_deviation((SELECT id FROM deviation WHERE trip_id=3),2,'2026-09-22 12:06','explained','Повтор')$$,'23514','already closed');
SELECT pg_temp.reject('Second current assignment (KP-01.3)', $$SELECT assign_trip(1,3,1,'2026-09-21 13:00','Без подмены')$$,'23514','already has a current assignment');
SELECT pg_temp.reject('Assignment to duty on line', (SELECT sql FROM prelude)||$$; SELECT assign_trip(4,1,1,'2026-09-22 09:55','Поздно')$$,'23514','only to a planned duty');
SELECT pg_temp.reject('Trip creation by dispatcher', $$SELECT create_trip(1,'2026-09-22 14:00',2)$$,'42501','Active admin required');
SELECT pg_temp.reject('Request id reused', $$SELECT release_duty(1,2,'2026-09-22 09:50','00000000-0000-0000-0000-00000000000a');
 SELECT cancel_duty(3,2,'2026-09-22 08:00','Резерв','00000000-0000-0000-0000-00000000000a')$$,'23505','reused with other parameters');
SELECT pg_temp.reject('Refresh overlaps next duty (KP-01.6)', $$SELECT unassign_trip(2,1,'2026-09-21 13:00','Перенос'); UPDATE duty SET vehicle_id=1 WHERE id=3;
 SELECT assign_trip(2,3,1,'2026-09-21 13:00','Перенос'); UPDATE route_point SET offset_seconds=3700 WHERE route_id=1 AND point_no=3;
 SELECT refresh_plan(1,1,'2026-09-22 08:00','Удлинение')$$,'23P01','duty_vehicle_id_plan_span_excl');
DO $$ BEGIN
 IF (SELECT count(*) FROM execution)<>0 OR (SELECT count(*) FROM duty WHERE status<>'planned')<>0
 OR (SELECT count(*) FROM assignment)<>3 OR (SELECT capacity FROM vehicle WHERE id=1)<>80
 OR (SELECT count(*) FROM trip_decision)<>0 OR (SELECT count(*) FROM deviation)<>0 OR (SELECT count(*) FROM passage)<>0
 OR (SELECT count(*) FROM operation_request)<>0 OR (SELECT count(*) FROM planned_trip WHERE refreshed_at IS NOT NULL)<>0
 OR (SELECT max(arrives_at) FROM trip_point WHERE trip_id=1)<>'2026-09-22 10:20'::timestamptz THEN
  RAISE EXCEPTION 'A failed operation leaked changes';
 END IF;
END $$;
ROLLBACK;
