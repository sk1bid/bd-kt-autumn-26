BEGIN;
SET LOCAL search_path=transport,public;
UPDATE device_installation SET ended_at='2026-09-22 08:00+07',ended_by=1,end_reason='Плановая перестановка' WHERE device_id='demo-device-1';
INSERT INTO device_installation(device_id,vehicle_id,started_at,created_by,reason)
 VALUES('demo-device-1',1,'2026-09-22 08:00+07',1,'Возврат после проверки');
UPDATE stop SET name='Новое название' WHERE id=1;
DO $$ BEGIN
 IF (SELECT stop_name_snapshot FROM trip_point WHERE trip_id=1 AND point_no=1)<>'Учебная площадь' THEN
  RAISE EXCEPTION 'Snapshot follows mutable stop';
 END IF;
END $$;
SET CONSTRAINTS ALL IMMEDIATE;
SELECT 'Adjacent installation periods; immutable snapshot origin: OK';
ROLLBACK;
