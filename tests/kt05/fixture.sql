-- Минимальный срез входов восьми отчётов в отдельной тестовой базе.
SET TIME ZONE 'UTC';
SET datestyle = 'ISO, YMD';
CREATE SCHEMA transport;
SET search_path = transport;
CREATE TABLE route (id int, number text);
CREATE TABLE planned_trip (id int, route_id int, starts_at timestamptz);
CREATE TABLE execution (trip_id int, status text);
CREATE TABLE trip_point (trip_id int, point_no int, arrives_at timestamptz);
CREATE TABLE passage (trip_id int, point_no int, arrived_at timestamptz);
CREATE TABLE passage_correction (trip_id int, point_no int, correction_no int, reliable boolean);
CREATE TABLE passage_assessment (trip_id int, point_no int, reliable boolean, delay_minutes numeric);
CREATE TABLE deviation (id int, trip_id int, point_no int, kind text, duty_id int,
                       detected_at timestamptz, closed_at timestamptz, measured_value int);
CREATE TABLE duty (id int, vehicle_id int, number text);
CREATE TABLE vehicle (id int, registration_no text);
CREATE TABLE device (id int, vendor text);
CREATE TABLE device_installation (id int, vehicle_id int);
CREATE TABLE message_key (device_id int, message_id int, first_received_at timestamptz,
                          generated_at timestamptz, result text, installation_id int);
INSERT INTO route VALUES (1,'A'),(2,'B');
INSERT INTO planned_trip VALUES (11,1,'2026-09-22 01:00'),(12,1,'2026-09-22 01:30'),
 (21,2,'2026-09-22 02:00'),(22,2,'2026-09-23 00:00');
INSERT INTO execution VALUES (11,'done'),(12,'waiting');
INSERT INTO trip_point VALUES (11,1,'2026-09-22 01:00'),(11,2,'2026-09-22 01:10'),
 (12,1,'2026-09-22 01:30'),(21,1,'2026-09-22 02:00');
INSERT INTO passage VALUES (11,1,'2026-09-22 01:00'),(11,2,'2026-09-22 01:20');
INSERT INTO passage_assessment VALUES (11,2,true,10);
INSERT INTO vehicle VALUES (1,'V1');
INSERT INTO duty VALUES (1,1,'D1');
INSERT INTO deviation VALUES (1,11,2,'late',1,'2026-09-22 01:20',NULL,10);
INSERT INTO device VALUES (1,'GPS');
INSERT INTO device_installation VALUES (1,1);
INSERT INTO message_key VALUES (1,1,'2026-09-22 01:00','2026-09-22 01:00','accepted',1),
 (1,2,'2026-09-22 01:05','2026-09-22 01:04','stale',1);
