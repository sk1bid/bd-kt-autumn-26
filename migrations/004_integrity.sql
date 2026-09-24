SET search_path = transport, public;

DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='transport_app') THEN CREATE ROLE transport_app NOLOGIN; END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='transport_telemetry') THEN CREATE ROLE transport_telemetry NOLOGIN; END IF;
END $$;

CREATE TABLE operation_request (
 request_id uuid PRIMARY KEY,
 operation text NOT NULL CHECK (operation IN ('create_trip','assign_trip','unassign_trip','refresh_plan','release_duty','close_duty','cancel_duty','replace_duty','decide_trip','close_deviation')),
 arguments jsonb NOT NULL,
 actor_id bigint NOT NULL REFERENCES app_user,
 applied_at timestamptz NOT NULL DEFAULT current_timestamp
);

ALTER TABLE deviation ADD CONSTRAINT deviation_connection_lost_duty_only CHECK (kind <> 'connection_lost' OR trip_id IS NULL);

CREATE TRIGGER request_immutable BEFORE UPDATE OR DELETE ON operation_request FOR EACH ROW EXECUTE FUNCTION immutable_row();
CREATE TRIGGER deviation_no_delete BEFORE DELETE ON deviation FOR EACH ROW EXECUTE FUNCTION immutable_row();

CREATE FUNCTION forbid_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'History cannot be truncated: %', TG_TABLE_NAME USING ERRCODE = '23514'; END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['planned_trip','trip_point','duty','assignment','device_installation','execution','trip_decision','passage','passage_correction','deviation','operation_request'] LOOP
  EXECUTE format('CREATE TRIGGER no_truncate BEFORE TRUNCATE ON transport.%I FOR EACH STATEMENT EXECUTE FUNCTION transport.forbid_truncate()', t);
 END LOOP;
END $$;

CREATE FUNCTION operation_once(request uuid, op text, actor bigint, args jsonb) RETURNS boolean
LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE inserted integer;
BEGIN
 IF request IS NULL THEN RETURN true; END IF;
 INSERT INTO operation_request(request_id,operation,arguments,actor_id) VALUES(request,op,args,actor) ON CONFLICT (request_id) DO NOTHING;
 GET DIAGNOSTICS inserted = ROW_COUNT;
 IF inserted = 1 THEN RETURN true; END IF;
 IF EXISTS (SELECT 1 FROM operation_request WHERE request_id=request AND operation=op AND actor_id=actor AND arguments=args) THEN RETURN false; END IF;
 RAISE EXCEPTION 'Request id reused with other parameters' USING ERRCODE='23505';
END $$;

CREATE FUNCTION check_model() RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
BEGIN
 IF EXISTS (
  SELECT r.id FROM route r LEFT JOIN route_point p ON p.route_id=r.id
  GROUP BY r.id HAVING count(p.point_no)<2 OR min(p.point_no)<>1 OR max(p.point_no)<>count(p.point_no)
 ) OR EXISTS (
  SELECT 1 FROM (SELECT point_no,offset_seconds,lag(offset_seconds) OVER (PARTITION BY route_id ORDER BY point_no) prev FROM route_point) p
  WHERE (point_no=1 AND offset_seconds<>0) OR offset_seconds<=prev
 ) THEN RAISE EXCEPTION 'Route requires consecutive points 1..N, N>=2 and increasing offsets from zero' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT t.id FROM planned_trip t LEFT JOIN trip_point p ON p.trip_id=t.id
  GROUP BY t.id HAVING count(p.point_no)<2 OR min(p.point_no)<>1 OR max(p.point_no)<>count(p.point_no)
     OR min(p.arrives_at)<>t.starts_at
 ) OR EXISTS (
  SELECT 1 FROM (SELECT arrives_at,lag(arrives_at) OVER (PARTITION BY trip_id ORDER BY point_no) prev FROM trip_point) p
  WHERE arrives_at<=prev
 ) THEN RAISE EXCEPTION 'Trip requires consecutive points and increasing times from its start' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM app_user u WHERE NOT EXISTS (SELECT 1 FROM user_role r WHERE r.user_id=u.id)) THEN
  RAISE EXCEPTION 'Every user needs at least one role' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM vehicle v WHERE NOT EXISTS (SELECT 1 FROM connection_state c WHERE c.vehicle_id=v.id)) THEN
  RAISE EXCEPTION 'Every vehicle needs a connection state' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM duty d WHERE d.status='planned' AND d.plan_span IS DISTINCT FROM (
   SELECT tstzrange(min(t.starts_at),max(p.arrives_at),'[)') FROM assignment a
   JOIN planned_trip t ON t.id=a.trip_id JOIN trip_point p ON p.trip_id=t.id
   WHERE a.duty_id=d.id AND a.ended_at IS NULL HAVING count(*)>0)
 ) THEN RAISE EXCEPTION 'Duty interval does not match its current assignments' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM assignment a JOIN duty d ON d.id=a.duty_id
  JOIN planned_trip t ON t.id=a.trip_id
  WHERE a.ended_at IS NULL AND ((t.starts_at AT TIME ZONE 'Asia/Novosibirsk')::date<>d.service_date OR d.status='cancelled')
 ) THEN RAISE EXCEPTION 'Assignment has wrong service date or cancelled duty' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM execution e JOIN planned_trip t ON t.id=e.trip_id
  WHERE t.frozen_at IS NULL OR NOT EXISTS (SELECT 1 FROM assignment a WHERE a.trip_id=e.trip_id AND a.ended_at IS NULL)
 ) OR EXISTS (
  SELECT 1 FROM planned_trip t WHERE t.frozen_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM execution e WHERE e.trip_id=t.id)
 ) THEN RAISE EXCEPTION 'Frozen plan and execution must be created together with an assignment' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT a.duty_id FROM assignment a JOIN execution e ON e.trip_id=a.trip_id
  WHERE a.ended_at IS NULL AND e.status='running' GROUP BY a.duty_id HAVING count(*)>1
 ) THEN RAISE EXCEPTION 'A duty has more than one running trip' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM execution e JOIN assignment a ON a.trip_id=e.trip_id AND a.ended_at IS NULL
  JOIN duty d ON d.id=a.duty_id WHERE (e.status IN ('waiting','running') AND d.status<>'on_line')
   OR (e.status='review' AND d.status='closed' AND NOT d.unchecked_results)
 ) THEN RAISE EXCEPTION 'Execution incompatible with duty state' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM duty d JOIN assignment a ON a.duty_id=d.id AND a.ended_at IS NULL
  LEFT JOIN execution e ON e.trip_id=a.trip_id
  WHERE d.closed_at IS NOT NULL AND (a.started_at>d.closed_at OR e.actual_start>d.closed_at OR e.actual_end>d.closed_at OR e.interrupted_at>d.closed_at)
 ) THEN RAISE EXCEPTION 'Duty closed before its trips ended' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM trip_decision x JOIN planned_trip t ON t.id=x.trip_id WHERE t.frozen_at IS NULL OR x.decided_at<t.frozen_at
 ) THEN RAISE EXCEPTION 'Decision precedes trip release' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM execution e WHERE e.completion_source='dispatcher' AND NOT EXISTS (
   SELECT 1 FROM trip_decision x WHERE x.trip_id=e.trip_id AND x.outcome='complete' AND x.confirmed_start=e.actual_start AND x.confirmed_end=e.actual_end)
 ) OR EXISTS (
  SELECT 1 FROM execution e WHERE e.status='failed' AND NOT EXISTS (
   SELECT 1 FROM trip_decision x WHERE x.trip_id=e.trip_id AND x.outcome='fail'
     AND x.confirmed_start IS NOT DISTINCT FROM e.actual_start AND x.interrupted_at IS NOT DISTINCT FROM e.interrupted_at)
 ) OR EXISTS (
  SELECT 1 FROM trip_decision x JOIN execution e USING(trip_id)
  WHERE (x.outcome='complete' AND (e.status<>'done' OR e.completion_source IS DISTINCT FROM 'dispatcher'
          OR e.actual_start IS DISTINCT FROM x.confirmed_start OR e.actual_end IS DISTINCT FROM x.confirmed_end))
     OR (x.outcome='fail' AND (e.status<>'failed' OR e.actual_start IS DISTINCT FROM x.confirmed_start
          OR e.interrupted_at IS DISTINCT FROM x.interrupted_at))
 ) OR EXISTS (
  SELECT 1 FROM execution e WHERE e.status='failed' AND NOT EXISTS (SELECT 1 FROM deviation v WHERE v.trip_id=e.trip_id AND v.kind='failed')
 ) THEN RAISE EXCEPTION 'Execution result does not match dispatcher decisions' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM execution e WHERE e.completion_source='telemetry' AND NOT EXISTS (
   SELECT 1 FROM passage p WHERE p.trip_id=e.trip_id AND p.arrived_at=e.actual_end
     AND p.point_no=(SELECT max(point_no) FROM trip_point WHERE trip_id=e.trip_id))
 ) THEN RAISE EXCEPTION 'Telemetry completion requires a passage of the last point' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM passage p JOIN execution e USING(trip_id)
  WHERE e.actual_start IS NULL OR p.arrived_at<e.actual_start OR p.arrived_at>e.actual_end OR p.arrived_at>e.interrupted_at
 ) OR EXISTS (
  SELECT 1 FROM passage p WHERE NOT EXISTS (
   SELECT 1 FROM assignment a JOIN duty d ON d.id=a.duty_id JOIN device_installation i ON i.vehicle_id=d.vehicle_id
   WHERE a.trip_id=p.trip_id AND tstzrange(a.started_at,a.ended_at,'[)') @> p.arrived_at
     AND i.device_id=p.source_device_id AND tstzrange(i.started_at,i.ended_at,'[)') @> p.arrived_at)
 ) THEN RAISE EXCEPTION 'Passage does not match execution period or duty vehicle device' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM deviation v WHERE v.close_reason='unreliable_data' AND coalesce((
   SELECT c.reliable FROM passage_correction c WHERE c.trip_id=v.trip_id AND c.point_no=v.point_no ORDER BY c.correction_no DESC LIMIT 1), true)
 ) THEN RAISE EXCEPTION 'Passage marked unreliable by deviation closure cannot be restored' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM deviation v WHERE v.trip_id IS NOT NULL AND NOT EXISTS (
   SELECT 1 FROM assignment a WHERE a.trip_id=v.trip_id AND a.duty_id=v.duty_id
     AND tstzrange(a.started_at,a.ended_at,'[)') @> v.detected_at)
 ) THEN RAISE EXCEPTION 'Deviation must refer to the assignment at detection time' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM message_key k JOIN device_installation i ON i.id=k.installation_id
  WHERE k.device_id<>i.device_id OR k.generated_at IS NULL OR NOT tstzrange(i.started_at,i.ended_at,'[)') @> k.generated_at
 ) THEN RAISE EXCEPTION 'Message installation does not match device/time' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM current_position p JOIN device_installation i ON i.id=p.installation_id
   WHERE p.vehicle_id<>i.vehicle_id OR NOT tstzrange(i.started_at,i.ended_at,'[)') @> p.generated_at)
 OR EXISTS (SELECT 1 FROM connection_state c JOIN device_installation i ON i.id=c.installation_id WHERE c.vehicle_id<>i.vehicle_id)
 THEN RAISE EXCEPTION 'Checkpoint references another vehicle or installation period' USING ERRCODE='23514'; END IF;
END $$;

CREATE OR REPLACE FUNCTION validate_model() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(20260922,1);
 PERFORM check_model();
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['trip_decision','passage','passage_correction'] LOOP
  EXECUTE format('CREATE CONSTRAINT TRIGGER model_consistency AFTER INSERT OR UPDATE OR DELETE ON transport.%I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION transport.validate_model()',t);
 END LOOP;
END $$;

CREATE FUNCTION guard_execution_start() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE duty_key bigint;
BEGIN
 SELECT duty_id INTO duty_key FROM assignment WHERE trip_id=NEW.trip_id AND ended_at IS NULL;
 PERFORM id FROM duty WHERE id=duty_key FOR UPDATE;
 IF NEW.status='running' AND OLD.status<>'running' THEN
  IF NOT EXISTS (SELECT 1 FROM duty WHERE id=duty_key AND status='on_line') THEN
   RAISE EXCEPTION 'Trip can start only in a duty on line' USING ERRCODE='23514'; END IF;
  IF EXISTS (SELECT 1 FROM assignment a JOIN execution x ON x.trip_id=a.trip_id
   WHERE a.duty_id=duty_key AND a.ended_at IS NULL AND x.trip_id<>NEW.trip_id AND x.status IN ('running','review')) THEN
   RAISE EXCEPTION 'Another running or review trip in the duty' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER execution_start BEFORE UPDATE ON execution FOR EACH ROW
 WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION guard_execution_start();

ALTER FUNCTION lock_planning() SECURITY DEFINER SET search_path=transport,pg_temp;
ALTER FUNCTION create_connection_state() SECURITY DEFINER SET search_path=transport,pg_temp;
ALTER FUNCTION refresh_duty_spans() SECURITY DEFINER SET search_path=transport,pg_temp;
ALTER FUNCTION preserve_history() SECURITY DEFINER;
ALTER FUNCTION protect_plan() SECURITY DEFINER SET search_path=transport,pg_temp;

DROP FUNCTION release_duty(bigint,bigint,timestamptz);
CREATE FUNCTION release_duty(duty_key bigint, actor bigint, at_time timestamptz, request uuid DEFAULT NULL) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE d duty; tr record;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
 IF NOT operation_once(request,'release_duty',actor,jsonb_build_array(duty_key,at_time AT TIME ZONE 'UTC')) THEN RETURN; END IF;
 SELECT * INTO STRICT d FROM duty WHERE id=duty_key FOR UPDATE;
 IF d.status<>'planned' THEN RAISE EXCEPTION 'Only planned duty can be released' USING ERRCODE='23514'; END IF;
 IF (at_time AT TIME ZONE 'Asia/Novosibirsk')::date<>d.service_date THEN RAISE EXCEPTION 'Wrong release date' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS (SELECT 1 FROM driver WHERE id=d.driver_id AND active AND admitted_until>=d.service_date)
  OR NOT EXISTS (SELECT 1 FROM vehicle WHERE id=d.vehicle_id AND active AND admitted_until>=d.service_date) THEN
  RAISE EXCEPTION 'Driver/vehicle not admitted' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS (SELECT 1 FROM device_installation i JOIN device x ON x.id=i.device_id
  WHERE i.vehicle_id=d.vehicle_id AND x.active AND tstzrange(i.started_at,i.ended_at,'[)') @> at_time) THEN
  RAISE EXCEPTION 'Installed active device required' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS (SELECT 1 FROM assignment WHERE duty_id=d.id AND ended_at IS NULL) THEN
  RAISE EXCEPTION 'Cannot release empty duty' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM assignment WHERE duty_id=d.id AND ended_at IS NULL AND started_at>at_time) THEN
  RAISE EXCEPTION 'Release cannot precede assignment' USING ERRCODE='23514'; END IF;
 FOR tr IN SELECT t.* FROM planned_trip t JOIN assignment a ON a.trip_id=t.id WHERE a.duty_id=d.id AND a.ended_at IS NULL ORDER BY t.id FOR UPDATE OF t LOOP
  IF EXISTS (SELECT 1 FROM execution WHERE trip_id=tr.id AND status<>'waiting') THEN RAISE EXCEPTION 'Only waiting trips can be released' USING ERRCODE='23514'; END IF;
  IF tr.refreshed_at>at_time THEN RAISE EXCEPTION 'Release cannot precede plan refresh' USING ERRCODE='23514'; END IF;
  IF tr.frozen_at IS NULL THEN UPDATE planned_trip SET frozen_at=at_time WHERE id=tr.id; END IF;
  INSERT INTO execution(trip_id,review_due_at,next_point_no) VALUES (tr.id,tr.starts_at+interval '15 minutes',1) ON CONFLICT(trip_id) DO NOTHING;
 END LOOP;
 UPDATE duty SET status='on_line',released_at=at_time WHERE id=d.id;
END $$;

DROP FUNCTION close_duty(bigint,bigint,timestamptz,text,boolean);
CREATE FUNCTION close_duty(duty_key bigint, actor bigint, at_time timestamptz, why text, allow_unknown boolean DEFAULT false, request uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE d duty;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
 IF NOT operation_once(request,'close_duty',actor,jsonb_build_array(duty_key,at_time AT TIME ZONE 'UTC',why,allow_unknown)) THEN RETURN; END IF;
 SELECT * INTO STRICT d FROM duty WHERE id=duty_key FOR UPDATE;
 IF d.status<>'on_line' OR why IS NULL OR btrim(why)='' THEN RAISE EXCEPTION 'Need active duty and reason' USING ERRCODE='23514'; END IF;
 IF at_time<d.released_at THEN RAISE EXCEPTION 'Closure cannot precede release' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM execution e JOIN assignment a ON a.trip_id=e.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND
    (e.status IN ('waiting','running') OR (e.status='review' AND NOT allow_unknown))) THEN
  RAISE EXCEPTION 'Unfinished trips block closure' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM assignment a JOIN execution e ON e.trip_id=a.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL
    AND greatest(e.actual_start,e.actual_end,e.interrupted_at)>at_time)
  OR EXISTS (SELECT 1 FROM assignment a JOIN trip_decision x ON x.trip_id=a.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND x.decided_at>at_time)
  OR EXISTS (SELECT 1 FROM assignment a WHERE a.duty_id=d.id AND a.ended_at IS NULL AND a.started_at>at_time) THEN
  RAISE EXCEPTION 'Closure time precedes trip results' USING ERRCODE='23514'; END IF;
 UPDATE duty SET status='closed',closed_at=at_time,closed_by=actor,close_reason=why,
  unchecked_results=EXISTS(SELECT 1 FROM execution e JOIN assignment a ON a.trip_id=e.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND e.status='review') WHERE id=d.id;
END $$;

CREATE FUNCTION cancel_duty(duty_key bigint, actor bigint, at_time timestamptz, why text, request uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE d duty;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
 IF NOT operation_once(request,'cancel_duty',actor,jsonb_build_array(duty_key,at_time AT TIME ZONE 'UTC',why)) THEN RETURN; END IF;
 SELECT * INTO STRICT d FROM duty WHERE id=duty_key FOR UPDATE;
 IF d.status<>'planned' OR why IS NULL OR btrim(why)='' THEN RAISE EXCEPTION 'Only planned duty can be cancelled with a reason' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM assignment WHERE duty_id=d.id AND ended_at IS NULL AND started_at>=at_time) THEN
  RAISE EXCEPTION 'Cancellation must follow assignments' USING ERRCODE='23514'; END IF;
 UPDATE assignment SET ended_at=at_time,ended_by=actor,end_reason=why WHERE duty_id=d.id AND ended_at IS NULL;
 UPDATE duty SET status='cancelled',closed_at=at_time,closed_by=actor,close_reason=why WHERE id=d.id;
END $$;

DROP FUNCTION decide_trip(bigint,bigint,timestamptz,text,text,timestamptz,timestamptz,timestamptz,integer);
CREATE FUNCTION decide_trip(trip_key bigint, actor bigint, at_time timestamptz, choice text, why text,
 started timestamptz DEFAULT NULL, finished timestamptz DEFAULT NULL, interrupted timestamptz DEFAULT NULL, next_point integer DEFAULT NULL, request uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE e execution; d duty; after_status text; due timestamptz; seq integer;
BEGIN
 PERFORM require_role(actor,'dispatcher');
 SELECT d0.* INTO STRICT d FROM duty d0 JOIN assignment a ON a.duty_id=d0.id WHERE a.trip_id=trip_key AND a.ended_at IS NULL FOR UPDATE OF d0;
 IF NOT operation_once(request,'decide_trip',actor,jsonb_build_array(trip_key,at_time AT TIME ZONE 'UTC',choice,why,
    started AT TIME ZONE 'UTC',finished AT TIME ZONE 'UTC',interrupted AT TIME ZONE 'UTC',next_point)) THEN RETURN; END IF;
 SELECT * INTO STRICT e FROM execution WHERE trip_id=trip_key FOR UPDATE;
 IF e.status NOT IN ('waiting','running','review') OR (choice<>'fail' AND e.status<>'review') THEN
  RAISE EXCEPTION 'Decision not allowed from this status' USING ERRCODE='23514'; END IF;
 IF d.released_at IS NULL OR at_time<d.released_at THEN
  RAISE EXCEPTION 'Decision cannot precede release' USING ERRCODE='23514'; END IF;
 IF d.status='closed' AND greatest(started,finished,interrupted)>d.closed_at THEN
  RAISE EXCEPTION 'Confirmed times cannot follow duty closure' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM trip_decision WHERE trip_id=trip_key AND decided_at>at_time) THEN
  RAISE EXCEPTION 'Decision time cannot move backwards' USING ERRCODE='23514'; END IF;
 IF choice='continue' THEN
  IF d.status<>'on_line' OR next_point IS NULL OR EXISTS (
   SELECT 1 FROM assignment a JOIN execution x ON x.trip_id=a.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND x.trip_id<>trip_key AND x.status IN ('running','review')
  ) THEN RAISE EXCEPTION 'Cannot continue this trip' USING ERRCODE='23514'; END IF;
  IF EXISTS (SELECT 1 FROM passage WHERE trip_id=trip_key AND point_no>=next_point) THEN RAISE EXCEPTION 'Next point already passed' USING ERRCODE='23514'; END IF;
  SELECT greatest(max(arrives_at)+interval '15 minutes',at_time+interval '15 minutes') INTO due FROM trip_point WHERE trip_id=trip_key;
 END IF;
 IF choice IN ('complete','continue','fail') AND e.actual_start IS NOT NULL AND started IS DISTINCT FROM e.actual_start THEN
  RAISE EXCEPTION 'Confirmed start conflicts with known start' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM passage p WHERE p.trip_id=trip_key AND NOT EXISTS (
    SELECT 1 FROM passage_correction c WHERE c.trip_id=p.trip_id AND c.point_no=p.point_no AND NOT c.reliable
    AND c.correction_no=(SELECT max(c2.correction_no) FROM passage_correction c2 WHERE c2.trip_id=c.trip_id AND c2.point_no=c.point_no))
    AND (p.arrived_at<started OR (choice='complete' AND p.arrived_at>finished) OR (choice='fail' AND (started IS NULL OR p.arrived_at>interrupted)))) THEN
  RAISE EXCEPTION 'Decision contradicts reliable passages' USING ERRCODE='23514'; END IF;
 after_status:=CASE choice WHEN 'continue' THEN 'running' WHEN 'complete' THEN 'done' WHEN 'fail' THEN 'failed' WHEN 'insufficient' THEN 'review' END;
 SELECT coalesce(max(decision_no),0)+1 INTO seq FROM trip_decision WHERE trip_id=trip_key;
 INSERT INTO trip_decision VALUES (trip_key,seq,actor,at_time,choice,why,e.status,after_status,started,finished,interrupted,next_point,due);
 IF choice<>'insufficient' THEN
  UPDATE execution SET status=after_status,actual_start=started,actual_end=finished,interrupted_at=interrupted,
   completion_source=CASE WHEN choice='complete' THEN 'dispatcher' END,
   next_point_no=next_point,review_due_at=coalesce(due,review_due_at) WHERE trip_id=trip_key;
 END IF;
 IF choice='fail' THEN INSERT INTO deviation(duty_id,trip_id,kind,detected_at) VALUES(d.id,trip_key,'failed',at_time); END IF;
END $$;

DROP FUNCTION close_deviation(bigint,bigint,timestamptz,text,text);
CREATE FUNCTION close_deviation(deviation_key bigint, actor bigint, at_time timestamptz, why text, note text, request uuid DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE v deviation; seq integer;
BEGIN
 PERFORM require_role(actor,'dispatcher');
 IF NOT operation_once(request,'close_deviation',actor,jsonb_build_array(deviation_key,at_time AT TIME ZONE 'UTC',why,note)) THEN RETURN; END IF;
 SELECT * INTO STRICT v FROM deviation WHERE id=deviation_key FOR UPDATE;
 IF v.closed_at IS NOT NULL THEN RAISE EXCEPTION 'Deviation already closed' USING ERRCODE='23514'; END IF;
 UPDATE deviation SET closed_at=at_time,closed_by=actor,close_reason=why,comment=note WHERE id=v.id;
 IF why='unreliable_data' THEN
  PERFORM 1 FROM passage WHERE trip_id=v.trip_id AND point_no=v.point_no FOR UPDATE;
  SELECT coalesce(max(correction_no),0)+1 INTO seq FROM passage_correction WHERE trip_id=v.trip_id AND point_no=v.point_no;
  INSERT INTO passage_correction VALUES(v.trip_id,v.point_no,seq,at_time,actor,false,note);
 END IF;
END $$;

DROP FUNCTION refresh_plan(bigint,bigint,timestamptz,text);
CREATE FUNCTION refresh_plan(trip_key bigint, actor bigint, at_time timestamptz, why text, request uuid DEFAULT NULL) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE t planned_trip;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'admin');
 IF NOT operation_once(request,'refresh_plan',actor,jsonb_build_array(trip_key,at_time AT TIME ZONE 'UTC',why)) THEN RETURN; END IF;
 SELECT * INTO STRICT t FROM planned_trip WHERE id=trip_key FOR UPDATE;
 IF t.frozen_at IS NOT NULL OR EXISTS (SELECT 1 FROM execution WHERE trip_id=trip_key) OR EXISTS (
  SELECT 1 FROM assignment a JOIN duty d ON d.id=a.duty_id WHERE a.trip_id=trip_key AND a.ended_at IS NULL AND d.status='on_line') THEN
  RAISE EXCEPTION 'Plan already released' USING ERRCODE='23514'; END IF;
 IF t.refreshed_at>at_time THEN RAISE EXCEPTION 'Refresh time cannot move backwards' USING ERRCODE='23514'; END IF;
 DELETE FROM trip_point WHERE trip_id=trip_key;
 INSERT INTO trip_point
  SELECT t.id,p.point_no,s.id,t.starts_at+make_interval(secs=>p.offset_seconds),s.name,s.latitude,s.longitude
  FROM route_point p JOIN stop s ON s.id=p.stop_id WHERE p.route_id=t.route_id;
 UPDATE planned_trip SET path_snapshot=(SELECT path FROM route WHERE id=t.route_id),refreshed_at=at_time,refreshed_by=actor,refresh_reason=why WHERE id=t.id;
END $$;

DROP FUNCTION replace_duty(bigint,bigint,bigint,timestamptz,text);
CREATE FUNCTION replace_duty(old_key bigint, new_key bigint, actor bigint, at_time timestamptz, why text, request uuid DEFAULT NULL) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE old_d duty; new_d duty; item assignment;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
 IF NOT operation_once(request,'replace_duty',actor,jsonb_build_array(old_key,new_key,at_time AT TIME ZONE 'UTC',why)) THEN RETURN; END IF;
 PERFORM id FROM duty WHERE id IN (old_key,new_key) ORDER BY id FOR UPDATE;
 SELECT * INTO STRICT old_d FROM duty WHERE id=old_key;
 SELECT * INTO STRICT new_d FROM duty WHERE id=new_key;
 IF old_key=new_key OR old_d.status<>'on_line' OR new_d.status<>'planned' OR old_d.service_date<>new_d.service_date THEN
  RAISE EXCEPTION 'Invalid replacement duties' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM assignment a JOIN execution e ON e.trip_id=a.trip_id WHERE a.duty_id=old_key AND a.ended_at IS NULL AND e.status IN ('running','review')) THEN
  RAISE EXCEPTION 'Replacement only between trips' USING ERRCODE='23514'; END IF;
 FOR item IN SELECT a0.* FROM assignment a0 JOIN execution e ON e.trip_id=a0.trip_id WHERE a0.duty_id=old_key AND a0.ended_at IS NULL AND e.status='waiting' ORDER BY a0.trip_id LOOP
  UPDATE assignment SET ended_at=at_time,ended_by=actor,end_reason=why WHERE id=item.id;
  INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES(item.trip_id,new_key,at_time,actor,why);
 END LOOP;
 PERFORM close_duty(old_key,actor,at_time,why);
 PERFORM release_duty(new_key,actor,at_time);
END $$;

CREATE FUNCTION create_trip(route_key bigint, start_at timestamptz, actor bigint, request uuid DEFAULT NULL) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
DECLARE trip_key bigint;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'admin');
 PERFORM operation_once(request,'create_trip',actor,jsonb_build_array(route_key,start_at AT TIME ZONE 'UTC'));
 SELECT id INTO trip_key FROM planned_trip WHERE route_id=route_key AND starts_at=start_at;
 IF trip_key IS NOT NULL THEN RETURN trip_key; END IF;
 IF NOT EXISTS (SELECT 1 FROM route WHERE id=route_key AND active) THEN
  RAISE EXCEPTION 'Active route required' USING ERRCODE='23514'; END IF;
 INSERT INTO planned_trip(route_id,starts_at,path_snapshot) SELECT id,start_at,path FROM route WHERE id=route_key RETURNING id INTO trip_key;
 INSERT INTO trip_point
  SELECT trip_key,p.point_no,s.id,start_at+make_interval(secs=>p.offset_seconds),s.name,s.latitude,s.longitude
  FROM route_point p JOIN stop s ON s.id=p.stop_id WHERE p.route_id=route_key;
 RETURN trip_key;
END $$;

CREATE FUNCTION assign_trip(trip_key bigint, duty_key bigint, actor bigint, at_time timestamptz, why text, request uuid DEFAULT NULL) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'admin');
 IF NOT operation_once(request,'assign_trip',actor,jsonb_build_array(trip_key,duty_key,at_time AT TIME ZONE 'UTC',why)) THEN RETURN; END IF;
 IF NOT EXISTS (SELECT 1 FROM duty WHERE id=duty_key AND status='planned') THEN
  RAISE EXCEPTION 'Trips are assigned only to a planned duty' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM assignment WHERE trip_id=trip_key AND ended_at IS NULL) THEN
  RAISE EXCEPTION 'Trip already has a current assignment; use replacement' USING ERRCODE='23514'; END IF;
 INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES(trip_key,duty_key,at_time,actor,why);
END $$;

CREATE FUNCTION unassign_trip(trip_key bigint, actor bigint, at_time timestamptz, why text, request uuid DEFAULT NULL) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=transport,pg_temp AS $$
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'admin');
 IF NOT operation_once(request,'unassign_trip',actor,jsonb_build_array(trip_key,at_time AT TIME ZONE 'UTC',why)) THEN RETURN; END IF;
 IF NOT EXISTS (SELECT 1 FROM assignment a JOIN duty d ON d.id=a.duty_id WHERE a.trip_id=trip_key AND a.ended_at IS NULL AND d.status='planned') THEN
  RAISE EXCEPTION 'Only an assignment of a planned duty can be ended' USING ERRCODE='23514'; END IF;
 UPDATE assignment SET ended_at=at_time,ended_by=actor,end_reason=why WHERE trip_id=trip_key AND ended_at IS NULL;
END $$;

CREATE FUNCTION schedule_report(period_start timestamptz, period_end timestamptz, route_key bigint DEFAULT NULL)
RETURNS TABLE(planned_trips bigint, completed bigint, completed_by_dispatcher bigint, failed bigint, interrupted bigint,
 needs_review bigint, pending bigint, not_released bigint, planned_points bigint, k bigint, n bigint,
 percent_on_time numeric, unreliable_points bigint, open_deviations bigint)
LANGUAGE sql STABLE SET search_path=transport,pg_temp AS $$
 WITH s AS (
  SELECT * FROM trip_summary t WHERE t.starts_at>=period_start AND t.starts_at<period_end AND (route_key IS NULL OR t.route_id=route_key)
 )
 SELECT count(*),
  count(*) FILTER (WHERE s.status='done'),
  count(*) FILTER (WHERE s.status='done' AND s.completion_source='dispatcher'),
  count(*) FILTER (WHERE s.status='failed'),
  count(*) FILTER (WHERE s.interrupted),
  count(*) FILTER (WHERE s.status='review'),
  count(*) FILTER (WHERE s.status IN ('waiting','running')),
  count(*) FILTER (WHERE s.status IS NULL),
  coalesce(sum(s.planned_points),0)::bigint, coalesce(sum(s.k),0)::bigint, coalesce(sum(s.n),0)::bigint,
  round(100.0*sum(s.k)/nullif(sum(s.n),0),1),
  coalesce(sum(s.unreliable_points),0)::bigint,
  (SELECT count(*) FROM deviation v WHERE v.closed_at IS NULL AND (v.trip_id IN (SELECT trip_id FROM s)
    OR (v.trip_id IS NULL AND route_key IS NULL AND v.detected_at>=period_start AND v.detected_at<period_end)))
 FROM s
$$;

REVOKE ALL ON SCHEMA transport FROM PUBLIC;
REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA transport FROM PUBLIC;
GRANT USAGE ON SCHEMA transport TO transport_app, transport_telemetry;
GRANT SELECT ON ALL TABLES IN SCHEMA transport TO transport_app, transport_telemetry;
GRANT INSERT, UPDATE ON driver, vehicle, stop, route, route_point, device, app_user TO transport_app;
GRANT DELETE ON route_point, user_role TO transport_app;
GRANT INSERT ON user_role, device_installation TO transport_app;
GRANT INSERT (number, service_date, vehicle_id, driver_id) ON duty TO transport_app;
GRANT UPDATE (ended_at, ended_by, end_reason) ON device_installation TO transport_app;
GRANT EXECUTE ON FUNCTION create_trip(bigint,timestamptz,bigint,uuid), assign_trip(bigint,bigint,bigint,timestamptz,text,uuid),
 unassign_trip(bigint,bigint,timestamptz,text,uuid), refresh_plan(bigint,bigint,timestamptz,text,uuid),
 release_duty(bigint,bigint,timestamptz,uuid), close_duty(bigint,bigint,timestamptz,text,boolean,uuid),
 cancel_duty(bigint,bigint,timestamptz,text,uuid), replace_duty(bigint,bigint,bigint,timestamptz,text,uuid),
 decide_trip(bigint,bigint,timestamptz,text,text,timestamptz,timestamptz,timestamptz,integer,uuid),
 close_deviation(bigint,bigint,timestamptz,text,text,uuid), schedule_report(timestamptz,timestamptz,bigint) TO transport_app;
GRANT INSERT ON passage, deviation, message_key, current_position TO transport_telemetry;
GRANT UPDATE (status, actual_start, actual_end, completion_source, next_point_no) ON execution TO transport_telemetry;
GRANT UPDATE ON message_key, current_position, connection_state TO transport_telemetry;

SELECT check_model();
