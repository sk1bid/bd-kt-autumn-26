CREATE SCHEMA transport_private;
REVOKE ALL ON SCHEMA transport_private FROM PUBLIC;
CREATE TABLE transport_private.model_check_queue (transaction_id bigint PRIMARY KEY);

CREATE FUNCTION transport_private.enqueue_model_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 INSERT INTO transport_private.model_check_queue VALUES (txid_current()) ON CONFLICT DO NOTHING;
 RETURN NULL;
END $$;

CREATE FUNCTION transport_private.run_model_check() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 DELETE FROM transport_private.model_check_queue WHERE transaction_id=NEW.transaction_id;
 PERFORM pg_advisory_xact_lock(20260922,1);
 PERFORM transport.check_model();
 RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER model_consistency
AFTER INSERT ON transport_private.model_check_queue
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
EXECUTE FUNCTION transport_private.run_model_check();

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['route','route_point','planned_trip','trip_point','app_user','user_role','vehicle','connection_state','duty','assignment','execution','deviation','message_key','current_position','device_installation','trip_decision','passage','passage_correction'] LOOP
  EXECUTE format('DROP TRIGGER model_consistency ON transport.%I',t);
  EXECUTE format('CREATE TRIGGER model_consistency AFTER INSERT OR UPDATE OR DELETE ON transport.%I FOR EACH STATEMENT EXECUTE FUNCTION transport_private.enqueue_model_check()',t);
 END LOOP;
END $$;

REVOKE ALL ON ALL TABLES IN SCHEMA transport_private FROM PUBLIC,transport_app,transport_telemetry;
REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA transport_private FROM PUBLIC,transport_app,transport_telemetry;
SELECT transport.check_model();
