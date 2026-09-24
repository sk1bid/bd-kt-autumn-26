CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE SCHEMA transport;
SET search_path = transport, public;

CREATE TABLE driver (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 license_no text NOT NULL UNIQUE CHECK (btrim(license_no) <> ''),
 full_name text NOT NULL CHECK (btrim(full_name) <> ''),
 admitted_until date NOT NULL, active boolean NOT NULL DEFAULT true
);
CREATE TABLE app_user (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 login text NOT NULL UNIQUE CHECK (login = lower(login) AND btrim(login) <> ''),
 full_name text NOT NULL CHECK (btrim(full_name) <> ''),
 driver_id bigint UNIQUE REFERENCES driver, active boolean NOT NULL DEFAULT true
);
CREATE TABLE role (
 code text PRIMARY KEY CHECK (code IN ('admin','dispatcher','driver','analyst'))
);
INSERT INTO role VALUES ('admin'), ('dispatcher'), ('driver'), ('analyst');
CREATE TABLE user_role (
 user_id bigint REFERENCES app_user, role_code text REFERENCES role,
 PRIMARY KEY (user_id, role_code)
);
CREATE TABLE stop (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 name text NOT NULL CHECK (btrim(name) <> ''),
 latitude numeric(9,6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
 longitude numeric(9,6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
 active boolean NOT NULL DEFAULT true,
 UNIQUE (name, latitude, longitude)
);
CREATE TABLE route (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 number text NOT NULL CHECK (btrim(number) <> ''),
 direction text NOT NULL CHECK (btrim(direction) <> ''),
 name text NOT NULL, transport_kind text NOT NULL CHECK (transport_kind IN ('bus','tram','trolleybus')),
 path path NOT NULL CHECK (npoints(path) >= 2), active boolean NOT NULL DEFAULT true,
 UNIQUE (number, direction)
);
CREATE TABLE route_point (
 route_id bigint REFERENCES route, point_no integer CHECK (point_no > 0),
 stop_id bigint NOT NULL REFERENCES stop,
 offset_seconds integer NOT NULL CHECK (offset_seconds >= 0),
 PRIMARY KEY (route_id, point_no)
);
CREATE TABLE vehicle (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 registration_no text NOT NULL UNIQUE CHECK (btrim(registration_no) <> ''),
 transport_kind text NOT NULL CHECK (transport_kind IN ('bus','tram','trolleybus')),
 capacity integer NOT NULL CHECK (capacity > 0),
 admitted_until date NOT NULL, active boolean NOT NULL DEFAULT true
);
CREATE TABLE device (
 id text PRIMARY KEY CHECK (btrim(id) <> ''),
 vendor text NOT NULL, message_format text NOT NULL,
 active boolean NOT NULL DEFAULT true
);
CREATE TABLE device_installation (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 device_id text NOT NULL REFERENCES device, vehicle_id bigint NOT NULL REFERENCES vehicle,
 started_at timestamptz NOT NULL, ended_at timestamptz,
 created_by bigint NOT NULL REFERENCES app_user, reason text NOT NULL CHECK (btrim(reason) <> ''),
 ended_by bigint REFERENCES app_user, end_reason text,
 CHECK (ended_at IS NULL OR ended_at > started_at),
 CHECK ((ended_at IS NULL AND ended_by IS NULL AND end_reason IS NULL) OR
        (ended_at IS NOT NULL AND ended_by IS NOT NULL AND btrim(end_reason) <> '' AND end_reason IS NOT NULL)),
 EXCLUDE USING gist (device_id WITH =, tstzrange(started_at,ended_at,'[)') WITH &&),
 EXCLUDE USING gist (vehicle_id WITH =, tstzrange(started_at,ended_at,'[)') WITH &&)
);
CREATE TABLE planned_trip (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 route_id bigint NOT NULL REFERENCES route, starts_at timestamptz NOT NULL,
 path_snapshot path NOT NULL CHECK (npoints(path_snapshot) >= 2),
 copied_at timestamptz NOT NULL DEFAULT current_timestamp,
 refreshed_at timestamptz, refreshed_by bigint REFERENCES app_user, refresh_reason text,
 frozen_at timestamptz,
 UNIQUE (route_id, starts_at),
 CHECK ((refreshed_at IS NULL AND refreshed_by IS NULL AND refresh_reason IS NULL) OR
        (refreshed_at IS NOT NULL AND refreshed_by IS NOT NULL AND refresh_reason IS NOT NULL AND btrim(refresh_reason) <> ''))
);
CREATE TABLE trip_point (
 trip_id bigint REFERENCES planned_trip, point_no integer CHECK (point_no > 0),
 stop_id bigint NOT NULL REFERENCES stop, arrives_at timestamptz NOT NULL,
 stop_name_snapshot text NOT NULL,
 latitude_snapshot numeric(9,6) NOT NULL CHECK (latitude_snapshot BETWEEN -90 AND 90),
 longitude_snapshot numeric(9,6) NOT NULL CHECK (longitude_snapshot BETWEEN -180 AND 180),
 PRIMARY KEY (trip_id, point_no)
);
CREATE TABLE duty (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 number text NOT NULL UNIQUE, service_date date NOT NULL,
 vehicle_id bigint NOT NULL REFERENCES vehicle, driver_id bigint NOT NULL REFERENCES driver,
 status text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','on_line','closed','cancelled')),
 plan_span tstzrange,
 released_at timestamptz, closed_at timestamptz, closed_by bigint REFERENCES app_user,
 close_reason text, unchecked_results boolean NOT NULL DEFAULT false,
 CHECK (plan_span IS NULL OR (NOT isempty(plan_span) AND NOT lower_inf(plan_span) AND NOT upper_inf(plan_span)
        AND lower_inc(plan_span) AND NOT upper_inc(plan_span))),
 CHECK ((status = 'planned' AND released_at IS NULL AND closed_at IS NULL) OR
        (status = 'on_line' AND released_at IS NOT NULL AND closed_at IS NULL) OR
        (status = 'closed' AND released_at IS NOT NULL AND closed_at IS NOT NULL AND closed_at >= released_at) OR
        (status = 'cancelled' AND released_at IS NULL AND closed_at IS NOT NULL)),
 CHECK ((closed_at IS NULL AND closed_by IS NULL AND close_reason IS NULL AND NOT unchecked_results) OR
        (closed_at IS NOT NULL AND closed_by IS NOT NULL AND close_reason IS NOT NULL AND btrim(close_reason) <> '')),
 EXCLUDE USING gist (vehicle_id WITH =, plan_span WITH &&) WHERE (status = 'planned'),
 EXCLUDE USING gist (driver_id WITH =, plan_span WITH &&) WHERE (status = 'planned')
);
CREATE UNIQUE INDEX one_vehicle_on_line ON duty(vehicle_id) WHERE status = 'on_line';
CREATE UNIQUE INDEX one_driver_on_line ON duty(driver_id) WHERE status = 'on_line';
CREATE TABLE assignment (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 trip_id bigint NOT NULL REFERENCES planned_trip, duty_id bigint NOT NULL REFERENCES duty,
 started_at timestamptz NOT NULL, ended_at timestamptz,
 created_by bigint NOT NULL REFERENCES app_user, reason text NOT NULL CHECK (btrim(reason) <> ''),
 ended_by bigint REFERENCES app_user, end_reason text,
 CHECK (ended_at IS NULL OR ended_at > started_at),
 CHECK ((ended_at IS NULL AND ended_by IS NULL AND end_reason IS NULL) OR
        (ended_at IS NOT NULL AND ended_by IS NOT NULL AND end_reason IS NOT NULL AND btrim(end_reason) <> '')),
 EXCLUDE USING gist (trip_id WITH =, tstzrange(started_at,ended_at,'[)') WITH &&)
);
CREATE UNIQUE INDEX one_current_assignment ON assignment(trip_id) WHERE ended_at IS NULL;
CREATE INDEX assignment_duty ON assignment(duty_id);
CREATE TABLE execution (
 trip_id bigint PRIMARY KEY REFERENCES planned_trip,
 status text NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting','running','review','done','failed')),
 actual_start timestamptz, actual_end timestamptz, interrupted_at timestamptz,
 completion_source text CHECK (completion_source IN ('telemetry','dispatcher')),
 next_point_no integer, review_due_at timestamptz NOT NULL,
 FOREIGN KEY (trip_id,next_point_no) REFERENCES trip_point,
 CHECK (actual_end IS NULL OR (actual_start IS NOT NULL AND actual_end >= actual_start)),
 CHECK (interrupted_at IS NULL OR (actual_start IS NOT NULL AND interrupted_at >= actual_start)),
 CHECK ((status = 'done' AND actual_start IS NOT NULL AND actual_end IS NOT NULL AND completion_source IS NOT NULL)
     OR (status <> 'done' AND actual_end IS NULL AND completion_source IS NULL)),
 CHECK (status <> 'waiting' OR (actual_start IS NULL AND interrupted_at IS NULL)),
 CHECK (status <> 'running' OR (actual_start IS NOT NULL AND next_point_no IS NOT NULL)),
 CHECK (status = 'failed' OR interrupted_at IS NULL),
 CHECK (status <> 'failed' OR ((actual_start IS NULL) = (interrupted_at IS NULL)))
);
CREATE TABLE trip_decision (
 trip_id bigint REFERENCES execution, decision_no integer CHECK (decision_no > 0),
 author_id bigint NOT NULL REFERENCES app_user, decided_at timestamptz NOT NULL,
 outcome text NOT NULL CHECK (outcome IN ('continue','complete','fail','insufficient')),
 reason text NOT NULL CHECK (btrim(reason) <> ''),
 status_before text NOT NULL CHECK (status_before IN ('waiting','running','review')),
 status_after text NOT NULL CHECK (status_after IN ('running','review','done','failed')),
 confirmed_start timestamptz, confirmed_end timestamptz, interrupted_at timestamptz,
 next_point_no integer, review_due_at timestamptz,
 PRIMARY KEY (trip_id, decision_no),
 FOREIGN KEY (trip_id,next_point_no) REFERENCES trip_point,
 CHECK (confirmed_start IS NULL OR confirmed_start <= decided_at),
 CHECK (confirmed_end IS NULL OR (confirmed_start IS NOT NULL AND confirmed_end BETWEEN confirmed_start AND decided_at)),
 CHECK (interrupted_at IS NULL OR (confirmed_start IS NOT NULL AND interrupted_at BETWEEN confirmed_start AND decided_at)),
 CHECK ((outcome = 'insufficient' AND status_before = 'review' AND status_after = 'review'
          AND confirmed_start IS NULL AND confirmed_end IS NULL AND interrupted_at IS NULL AND next_point_no IS NULL AND review_due_at IS NULL)
     OR (outcome = 'complete' AND status_before = 'review' AND status_after = 'done' AND confirmed_start IS NOT NULL AND confirmed_end IS NOT NULL AND interrupted_at IS NULL AND next_point_no IS NULL AND review_due_at IS NULL)
     OR (outcome = 'continue' AND status_before = 'review' AND status_after = 'running' AND confirmed_start IS NOT NULL AND confirmed_end IS NULL AND interrupted_at IS NULL AND next_point_no IS NOT NULL AND review_due_at IS NOT NULL AND review_due_at > decided_at)
     OR (outcome = 'fail' AND status_after = 'failed' AND confirmed_end IS NULL AND next_point_no IS NULL AND review_due_at IS NULL AND ((confirmed_start IS NULL) = (interrupted_at IS NULL))))
);
CREATE TABLE passage (
 trip_id bigint REFERENCES execution, point_no integer,
 arrived_at timestamptz NOT NULL,
 source_device_id text NOT NULL REFERENCES device, source_message_id uuid NOT NULL,
 evidence text NOT NULL CHECK (btrim(evidence) <> ''),
 PRIMARY KEY (trip_id, point_no), FOREIGN KEY (trip_id,point_no) REFERENCES trip_point,
 UNIQUE (source_device_id,source_message_id)
);
CREATE TABLE passage_correction (
 trip_id bigint, point_no integer, correction_no integer CHECK (correction_no > 0),
 corrected_at timestamptz NOT NULL, author_id bigint NOT NULL REFERENCES app_user,
 reliable boolean NOT NULL, reason text NOT NULL CHECK (btrim(reason) <> ''),
 PRIMARY KEY (trip_id,point_no,correction_no), FOREIGN KEY (trip_id,point_no) REFERENCES passage
);
CREATE TABLE deviation (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 duty_id bigint NOT NULL REFERENCES duty, trip_id bigint REFERENCES execution, point_no integer,
 kind text NOT NULL CHECK (kind IN ('late','early','off_route','connection_lost','failed')),
 detected_at timestamptz NOT NULL, measured_value numeric,
 episode_started_at timestamptz,
 closed_at timestamptz, closed_by bigint REFERENCES app_user,
 close_reason text CHECK (close_reason IN ('explained','replacement','unreliable_data')), comment text,
 FOREIGN KEY (trip_id, point_no) REFERENCES passage,
 CHECK ((kind IN ('late','early') AND trip_id IS NOT NULL AND point_no IS NOT NULL AND measured_value IS NOT NULL)
     OR (kind IN ('off_route','failed') AND trip_id IS NOT NULL AND point_no IS NULL)
     OR (kind = 'connection_lost' AND point_no IS NULL AND episode_started_at IS NOT NULL)),
 CHECK (episode_started_at IS NULL OR episode_started_at <= detected_at),
 CHECK ((closed_at IS NULL AND closed_by IS NULL AND close_reason IS NULL AND comment IS NULL) OR
        (closed_at IS NOT NULL AND closed_at >= detected_at AND closed_by IS NOT NULL AND close_reason IS NOT NULL AND comment IS NOT NULL AND btrim(comment) <> '')),
 CHECK (close_reason IS DISTINCT FROM 'unreliable_data' OR kind IN ('late','early'))
);
CREATE UNIQUE INDEX one_timing_deviation ON deviation(trip_id,point_no) WHERE kind IN ('late','early');
CREATE UNIQUE INDEX one_trip_deviation ON deviation(trip_id,kind) WHERE kind IN ('off_route','failed');
CREATE UNIQUE INDEX one_connection_episode ON deviation(duty_id,episode_started_at) WHERE kind = 'connection_lost';
CREATE INDEX deviation_duty ON deviation(duty_id);
CREATE TABLE message_key (
 device_id text REFERENCES device, message_id uuid,
 content_sha256 bytea NOT NULL CHECK (octet_length(content_sha256) = 32),
 first_received_at timestamptz NOT NULL, generated_at timestamptz,
 installation_id bigint REFERENCES device_installation,
 first_receipt_id uuid NOT NULL UNIQUE, result text NOT NULL CHECK (result IN ('accepted','invalid','stale')),
 PRIMARY KEY (device_id,message_id)
);
CREATE INDEX message_key_retention ON message_key(first_received_at);
CREATE TABLE current_position (
 vehicle_id bigint PRIMARY KEY REFERENCES vehicle,
 installation_id bigint NOT NULL REFERENCES device_installation,
 message_id uuid NOT NULL, generated_at timestamptz NOT NULL,
 latitude numeric(9,6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
 longitude numeric(9,6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
 speed_kmh numeric(7,2) CHECK (speed_kmh >= 0),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)
);
CREATE TABLE connection_state (
 vehicle_id bigint PRIMARY KEY REFERENCES vehicle,
 last_confirmed_at timestamptz, installation_id bigint REFERENCES device_installation,
 message_id uuid, revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 CHECK ((last_confirmed_at IS NULL AND installation_id IS NULL AND message_id IS NULL) OR
        (last_confirmed_at IS NOT NULL AND installation_id IS NOT NULL AND message_id IS NOT NULL))
);
CREATE TABLE planning_guard (id integer PRIMARY KEY CHECK (id = 1));
INSERT INTO planning_guard VALUES (1);
