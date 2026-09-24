SET search_path = transport, public;

CREATE FUNCTION lock_planning() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM transport.planning_guard WHERE id = 1 FOR UPDATE;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['route','route_point','planned_trip','trip_point','duty','assignment','device_installation','vehicle','driver','device'] LOOP
  EXECUTE format('CREATE TRIGGER planning_lock BEFORE INSERT OR UPDATE OR DELETE ON transport.%I FOR EACH STATEMENT EXECUTE FUNCTION transport.lock_planning()', t);
 END LOOP;
END $$;

CREATE FUNCTION immutable_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'History is append-only: %', TG_TABLE_NAME USING ERRCODE = '23514'; END $$;
CREATE TRIGGER decision_immutable BEFORE UPDATE OR DELETE ON trip_decision FOR EACH ROW EXECUTE FUNCTION immutable_row();
CREATE TRIGGER correction_immutable BEFORE UPDATE OR DELETE ON passage_correction FOR EACH ROW EXECUTE FUNCTION immutable_row();
CREATE TRIGGER passage_immutable BEFORE UPDATE OR DELETE ON passage FOR EACH ROW EXECUTE FUNCTION immutable_row();

CREATE FUNCTION protect_plan() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE frozen timestamptz;
BEGIN
 IF TG_TABLE_NAME = 'planned_trip' THEN
  IF OLD.frozen_at IS NOT NULL THEN
   RAISE EXCEPTION 'Frozen trip cannot change' USING ERRCODE = '23514';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.route_id,NEW.starts_at) IS DISTINCT FROM (OLD.route_id,OLD.starts_at) THEN
   RAISE EXCEPTION 'Create another trip for a different route or start' USING ERRCODE = '23514';
  END IF;
 ELSE
  IF TG_OP <> 'INSERT' THEN
   SELECT frozen_at INTO frozen FROM transport.planned_trip WHERE id = OLD.trip_id;
   IF frozen IS NOT NULL THEN RAISE EXCEPTION 'Frozen trip points cannot change' USING ERRCODE = '23514'; END IF;
  END IF;
  IF TG_OP <> 'DELETE' THEN
   SELECT frozen_at INTO frozen FROM transport.planned_trip WHERE id = NEW.trip_id;
   IF frozen IS NOT NULL THEN RAISE EXCEPTION 'Frozen trip points cannot change' USING ERRCODE = '23514'; END IF;
  END IF;
 END IF;
 IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_trip BEFORE UPDATE OR DELETE ON planned_trip FOR EACH ROW EXECUTE FUNCTION protect_plan();
CREATE TRIGGER protect_points BEFORE INSERT OR UPDATE OR DELETE ON trip_point FOR EACH ROW EXECUTE FUNCTION protect_plan();

CREATE FUNCTION create_connection_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO transport.connection_state(vehicle_id) VALUES (NEW.id); RETURN NEW; END $$;
CREATE TRIGGER vehicle_connection AFTER INSERT ON vehicle FOR EACH ROW EXECUTE FUNCTION create_connection_state();

CREATE FUNCTION refresh_duty_spans() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE transport.duty d SET plan_span = (
  SELECT tstzrange(min(t.starts_at), max(p.arrives_at), '[)')
  FROM transport.assignment a JOIN transport.planned_trip t ON t.id=a.trip_id
  JOIN transport.trip_point p ON p.trip_id=t.id
  WHERE a.duty_id=d.id AND a.ended_at IS NULL HAVING count(*) > 0
 ) WHERE d.status = 'planned';
 RETURN NULL;
END $$;
CREATE TRIGGER assignment_span AFTER INSERT OR UPDATE OR DELETE ON assignment FOR EACH STATEMENT EXECUTE FUNCTION refresh_duty_spans();
CREATE TRIGGER point_span AFTER INSERT OR UPDATE OR DELETE ON trip_point FOR EACH STATEMENT EXECUTE FUNCTION refresh_duty_spans();

CREATE FUNCTION validate_model() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT r.id FROM transport.route r LEFT JOIN transport.route_point p ON p.route_id=r.id
  GROUP BY r.id HAVING count(p.point_no)<2 OR min(p.point_no)<>1 OR max(p.point_no)<>count(p.point_no)
 ) OR EXISTS (
  SELECT 1 FROM (SELECT point_no,offset_seconds,lag(offset_seconds) OVER (PARTITION BY route_id ORDER BY point_no) prev FROM transport.route_point) p
  WHERE (point_no=1 AND offset_seconds<>0) OR offset_seconds<=prev
 ) THEN RAISE EXCEPTION 'Route requires consecutive points 1..N, N>=2 and increasing offsets from zero' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT t.id FROM transport.planned_trip t LEFT JOIN transport.trip_point p ON p.trip_id=t.id
  GROUP BY t.id HAVING count(p.point_no)<2 OR min(p.point_no)<>1 OR max(p.point_no)<>count(p.point_no)
     OR min(p.arrives_at)<>t.starts_at
 ) OR EXISTS (
  SELECT 1 FROM (SELECT arrives_at,lag(arrives_at) OVER (PARTITION BY trip_id ORDER BY point_no) prev FROM transport.trip_point) p
  WHERE arrives_at<=prev
 ) THEN RAISE EXCEPTION 'Trip requires consecutive points and increasing times from its start' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM transport.app_user u WHERE NOT EXISTS (SELECT 1 FROM transport.user_role r WHERE r.user_id=u.id)) THEN
  RAISE EXCEPTION 'Every user needs at least one role' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM transport.vehicle v WHERE NOT EXISTS (SELECT 1 FROM transport.connection_state c WHERE c.vehicle_id=v.id)) THEN
  RAISE EXCEPTION 'Every vehicle needs a connection state' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.duty d WHERE d.status='planned' AND d.plan_span IS DISTINCT FROM (
   SELECT tstzrange(min(t.starts_at),max(p.arrives_at),'[)') FROM transport.assignment a
   JOIN transport.planned_trip t ON t.id=a.trip_id JOIN transport.trip_point p ON p.trip_id=t.id
   WHERE a.duty_id=d.id AND a.ended_at IS NULL HAVING count(*)>0)
 ) THEN RAISE EXCEPTION 'Duty interval does not match its current assignments' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.assignment a JOIN transport.duty d ON d.id=a.duty_id
  JOIN transport.planned_trip t ON t.id=a.trip_id
  WHERE a.ended_at IS NULL AND ((t.starts_at AT TIME ZONE 'Asia/Novosibirsk')::date<>d.service_date OR d.status='cancelled')
 ) THEN RAISE EXCEPTION 'Assignment has wrong service date or cancelled duty' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.execution e JOIN transport.planned_trip t ON t.id=e.trip_id
  WHERE t.frozen_at IS NULL OR NOT EXISTS (SELECT 1 FROM transport.assignment a WHERE a.trip_id=e.trip_id AND a.ended_at IS NULL)
 ) OR EXISTS (
  SELECT 1 FROM transport.planned_trip t WHERE t.frozen_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM transport.execution e WHERE e.trip_id=t.id)
 ) THEN RAISE EXCEPTION 'Frozen plan and execution must be created together with an assignment' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT a.duty_id FROM transport.assignment a JOIN transport.execution e ON e.trip_id=a.trip_id
  WHERE a.ended_at IS NULL AND e.status='running' GROUP BY a.duty_id HAVING count(*)>1
 ) THEN RAISE EXCEPTION 'A duty has another running or review trip' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.execution e JOIN transport.assignment a ON a.trip_id=e.trip_id AND a.ended_at IS NULL
  JOIN transport.duty d ON d.id=a.duty_id WHERE (e.status IN ('waiting','running') AND d.status<>'on_line')
   OR (e.status='review' AND d.status='closed' AND NOT d.unchecked_results)
 ) THEN RAISE EXCEPTION 'Execution incompatible with duty state' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.deviation v WHERE v.trip_id IS NOT NULL AND NOT EXISTS (
   SELECT 1 FROM transport.assignment a WHERE a.trip_id=v.trip_id AND a.duty_id=v.duty_id
     AND tstzrange(a.started_at,a.ended_at,'[)') @> v.detected_at)
 ) THEN RAISE EXCEPTION 'Deviation must refer to the assignment at detection time' USING ERRCODE='23514'; END IF;
 IF EXISTS (
  SELECT 1 FROM transport.message_key k JOIN transport.device_installation i ON i.id=k.installation_id
  WHERE k.device_id<>i.device_id OR k.generated_at IS NULL OR NOT tstzrange(i.started_at,i.ended_at,'[)') @> k.generated_at
 ) THEN RAISE EXCEPTION 'Message installation does not match device/time' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM transport.current_position p JOIN transport.device_installation i ON i.id=p.installation_id
   WHERE p.vehicle_id<>i.vehicle_id OR NOT tstzrange(i.started_at,i.ended_at,'[)') @> p.generated_at)
 OR EXISTS (SELECT 1 FROM transport.connection_state c JOIN transport.device_installation i ON i.id=c.installation_id WHERE c.vehicle_id<>i.vehicle_id)
 THEN RAISE EXCEPTION 'Checkpoint references another vehicle or installation period' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['route','route_point','planned_trip','trip_point','app_user','user_role','vehicle','connection_state','duty','assignment','execution','deviation','message_key','current_position','device_installation'] LOOP
  EXECUTE format('CREATE CONSTRAINT TRIGGER model_consistency AFTER INSERT OR UPDATE OR DELETE ON transport.%I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION transport.validate_model()',t);
 END LOOP;
END $$;

CREATE FUNCTION require_role(actor bigint, needed text) RETURNS void LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM app_user u JOIN user_role r ON r.user_id=u.id WHERE u.id=actor AND u.active AND r.role_code=needed) THEN
  RAISE EXCEPTION 'Active % required',needed USING ERRCODE='42501';
 END IF;
END $$;

CREATE FUNCTION release_duty(duty_key bigint, actor bigint, at_time timestamptz) RETURNS void
LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE d duty; tr record;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
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
 FOR tr IN SELECT t.* FROM planned_trip t JOIN assignment a ON a.trip_id=t.id WHERE a.duty_id=d.id AND a.ended_at IS NULL ORDER BY t.id FOR UPDATE OF t LOOP
  IF EXISTS (SELECT 1 FROM execution WHERE trip_id=tr.id AND status<>'waiting') THEN RAISE EXCEPTION 'Only waiting trips can be released' USING ERRCODE='23514'; END IF;
  IF tr.frozen_at IS NULL THEN UPDATE planned_trip SET frozen_at=at_time WHERE id=tr.id; END IF;
  INSERT INTO execution(trip_id,review_due_at,next_point_no) VALUES (tr.id,tr.starts_at+interval '15 minutes',1) ON CONFLICT(trip_id) DO NOTHING;
 END LOOP;
 UPDATE duty SET status='on_line',released_at=at_time WHERE id=d.id;
END $$;

CREATE FUNCTION close_duty(duty_key bigint, actor bigint, at_time timestamptz, why text, allow_unknown boolean DEFAULT false)
RETURNS void LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE d duty;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
 SELECT * INTO STRICT d FROM duty WHERE id=duty_key FOR UPDATE;
 IF d.status<>'on_line' OR why IS NULL OR btrim(why)='' THEN RAISE EXCEPTION 'Need active duty and reason' USING ERRCODE='23514'; END IF;
 IF EXISTS (SELECT 1 FROM execution e JOIN assignment a ON a.trip_id=e.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND
    (e.status IN ('waiting','running') OR (e.status='review' AND NOT allow_unknown))) THEN
  RAISE EXCEPTION 'Unfinished trips block closure' USING ERRCODE='23514'; END IF;
 UPDATE duty SET status='closed',closed_at=at_time,closed_by=actor,close_reason=why,
  unchecked_results=EXISTS(SELECT 1 FROM execution e JOIN assignment a ON a.trip_id=e.trip_id WHERE a.duty_id=d.id AND a.ended_at IS NULL AND e.status='review') WHERE id=d.id;
END $$;

CREATE FUNCTION decide_trip(trip_key bigint, actor bigint, at_time timestamptz, choice text, why text,
 started timestamptz DEFAULT NULL, finished timestamptz DEFAULT NULL, interrupted timestamptz DEFAULT NULL, next_point integer DEFAULT NULL)
RETURNS void LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE e execution; d duty; after_status text; due timestamptz; seq integer;
BEGIN
 PERFORM require_role(actor,'dispatcher');
 SELECT d0.* INTO STRICT d FROM duty d0 JOIN assignment a ON a.duty_id=d0.id WHERE a.trip_id=trip_key AND a.ended_at IS NULL FOR UPDATE OF d0;
 SELECT * INTO STRICT e FROM execution WHERE trip_id=trip_key FOR UPDATE;
 IF e.status NOT IN ('waiting','running','review') OR (choice<>'fail' AND e.status<>'review') THEN
  RAISE EXCEPTION 'Decision not allowed from this status' USING ERRCODE='23514'; END IF;
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

CREATE FUNCTION close_deviation(deviation_key bigint, actor bigint, at_time timestamptz, why text, note text)
RETURNS void LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE v deviation; seq integer;
BEGIN
 PERFORM require_role(actor,'dispatcher');
 SELECT * INTO STRICT v FROM deviation WHERE id=deviation_key FOR UPDATE;
 IF v.closed_at IS NOT NULL THEN RAISE EXCEPTION 'Deviation already closed' USING ERRCODE='23514'; END IF;
 UPDATE deviation SET closed_at=at_time,closed_by=actor,close_reason=why,comment=note WHERE id=v.id;
 IF why='unreliable_data' THEN
  PERFORM 1 FROM passage WHERE trip_id=v.trip_id AND point_no=v.point_no FOR UPDATE;
  SELECT coalesce(max(correction_no),0)+1 INTO seq FROM passage_correction WHERE trip_id=v.trip_id AND point_no=v.point_no;
  INSERT INTO passage_correction VALUES(v.trip_id,v.point_no,seq,at_time,actor,false,note);
 END IF;
END $$;

CREATE FUNCTION protect_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='duty' THEN
  IF NOT ((OLD.status='planned' AND NEW.status IN ('planned','on_line','cancelled')) OR
    (OLD.status='on_line' AND NEW.status IN ('on_line','closed')) OR NEW IS NOT DISTINCT FROM OLD) THEN
   RAISE EXCEPTION 'Invalid duty transition' USING ERRCODE='23514';
  END IF;
 ELSIF TG_TABLE_NAME='execution' THEN
  IF NOT ((OLD.status='waiting' AND NEW.status IN ('waiting','running','review','failed')) OR
   (OLD.status='running' AND NEW.status IN ('running','review','done','failed')) OR
   (OLD.status='review' AND NEW.status IN ('review','running','done','failed')) OR NEW IS NOT DISTINCT FROM OLD) THEN
   RAISE EXCEPTION 'Invalid execution transition' USING ERRCODE='23514';
  END IF;
 ELSIF TG_TABLE_NAME='deviation' THEN
  IF OLD.closed_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'Closed deviation is immutable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER duty_transition BEFORE UPDATE ON duty FOR EACH ROW EXECUTE FUNCTION protect_transitions();
CREATE TRIGGER execution_transition BEFORE UPDATE ON execution FOR EACH ROW EXECUTE FUNCTION protect_transitions();
CREATE TRIGGER deviation_transition BEFORE UPDATE ON deviation FOR EACH ROW EXECUTE FUNCTION protect_transitions();

CREATE FUNCTION preserve_history() RETURNS trigger LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'History cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.ended_at IS NOT NULL OR
     (to_jsonb(NEW)-ARRAY['ended_at','ended_by','end_reason']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['ended_at','ended_by','end_reason']) OR NEW.ended_at IS NULL THEN
   RAISE EXCEPTION 'Only closing an open history interval is allowed' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_TABLE_NAME='device_installation' THEN
  IF EXISTS (SELECT 1 FROM duty WHERE vehicle_id=NEW.vehicle_id AND status='on_line') THEN
   RAISE EXCEPTION 'Cannot change device on a vehicle on line' USING ERRCODE='23514'; END IF;
 ELSE
  IF EXISTS (SELECT 1 FROM execution WHERE trip_id=NEW.trip_id AND status<>'waiting') THEN
   RAISE EXCEPTION 'Only waiting trips can be reassigned' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER installation_history BEFORE INSERT OR UPDATE OR DELETE ON device_installation FOR EACH ROW EXECUTE FUNCTION preserve_history();
CREATE TRIGGER assignment_history BEFORE INSERT OR UPDATE OR DELETE ON assignment FOR EACH ROW EXECUTE FUNCTION preserve_history();

CREATE FUNCTION refresh_plan(trip_key bigint, actor bigint, at_time timestamptz, why text) RETURNS void
LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE t planned_trip;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'admin');
 SELECT * INTO STRICT t FROM planned_trip WHERE id=trip_key FOR UPDATE;
 IF t.frozen_at IS NOT NULL OR EXISTS (SELECT 1 FROM execution WHERE trip_id=trip_key) OR EXISTS (
  SELECT 1 FROM assignment a JOIN duty d ON d.id=a.duty_id WHERE a.trip_id=trip_key AND a.ended_at IS NULL AND d.status='on_line') THEN
  RAISE EXCEPTION 'Plan already released' USING ERRCODE='23514'; END IF;
 DELETE FROM trip_point WHERE trip_id=trip_key;
 INSERT INTO trip_point
  SELECT t.id,p.point_no,s.id,t.starts_at+make_interval(secs=>p.offset_seconds),s.name,s.latitude,s.longitude
  FROM route_point p JOIN stop s ON s.id=p.stop_id WHERE p.route_id=t.route_id;
 UPDATE planned_trip SET path_snapshot=(SELECT path FROM route WHERE id=t.route_id),refreshed_at=at_time,refreshed_by=actor,refresh_reason=why WHERE id=t.id;
END $$;

CREATE FUNCTION replace_duty(old_key bigint, new_key bigint, actor bigint, at_time timestamptz, why text) RETURNS void
LANGUAGE plpgsql SET search_path=transport,pg_temp AS $$
DECLARE old_d duty; new_d duty; item assignment;
BEGIN
 PERFORM id FROM planning_guard WHERE id=1 FOR UPDATE;
 PERFORM require_role(actor,'dispatcher');
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
