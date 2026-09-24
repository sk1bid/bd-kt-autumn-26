BEGIN;
SET LOCAL search_path=transport,public;
SET LOCAL TIME ZONE 'Asia/Novosibirsk';
INSERT INTO driver(license_no,full_name,admitted_until) VALUES
 ('DEMO-001','Водитель Первый','2026-12-31'),('DEMO-002','Водитель Второй','2026-12-31'),('DEMO-003','Водитель Третий','2026-12-31');
INSERT INTO app_user(login,full_name) VALUES ('admin','Администратор НСИ'),('dispatcher','Диспетчер смены'),('analyst','Аналитик');
INSERT INTO user_role VALUES(1,'admin'),(2,'dispatcher'),(3,'analyst');
INSERT INTO stop(name,latitude,longitude) VALUES
 ('Учебная площадь',55.030000,82.920000),('Учебный парк',55.040000,82.930000),('Учебный вокзал',55.050000,82.940000);
INSERT INTO route(number,direction,name,transport_kind,path) VALUES
 ('ДЕМО-1','прямое','Площадь - вокзал','bus','[(82.92,55.03),(82.93,55.04),(82.94,55.05)]');
INSERT INTO route_point VALUES (1,1,1,0),(1,2,2,600),(1,3,3,1200);
INSERT INTO vehicle(registration_no,transport_kind,capacity,admitted_until) VALUES
 ('DEMO-BUS-1','bus',80,'2026-12-31'),('DEMO-BUS-2','bus',60,'2026-12-31'),('DEMO-BUS-3','bus',40,'2026-12-31');
INSERT INTO device VALUES ('demo-device-1','vendor-a','a-v1',true),('demo-device-2','vendor-b','b-v1',true),('demo-device-3','vendor-a','a-v1',true);
INSERT INTO device_installation(device_id,vehicle_id,started_at,created_by,reason)
 SELECT 'demo-device-'||id,id,'2026-09-01 00:00+07',1,'Начальная установка учебного устройства' FROM vehicle;
INSERT INTO planned_trip(route_id,starts_at,path_snapshot)
 SELECT 1, x, path FROM route CROSS JOIN unnest(ARRAY['2026-09-22 10:00+07'::timestamptz,'2026-09-22 11:00+07'::timestamptz,'2026-09-22 12:00+07'::timestamptz,'2026-09-22 13:00+07'::timestamptz]) x;
INSERT INTO trip_point
 SELECT t.id,p.point_no,s.id,t.starts_at+make_interval(secs=>p.offset_seconds),s.name,s.latitude,s.longitude
 FROM planned_trip t JOIN route_point p ON p.route_id=t.route_id JOIN stop s ON s.id=p.stop_id;
INSERT INTO duty(number,service_date,vehicle_id,driver_id) VALUES
 ('Н-001','2026-09-22',1,1),('Н-002','2026-09-22',2,2),('Н-003','2026-09-22',3,3);
INSERT INTO assignment(trip_id,duty_id,started_at,created_by,reason) VALUES
 (1,1,'2026-09-21 12:00+07',1,'План смены'),(2,1,'2026-09-21 12:00+07',1,'План смены'),
 (3,2,'2026-09-21 12:00+07',1,'План смены');
COMMIT;
