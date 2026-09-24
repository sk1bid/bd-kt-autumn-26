BEGIN;
SET LOCAL search_path=transport,public;
SET LOCAL TIME ZONE 'Asia/Novosibirsk';
SELECT release_duty(1,2,'2026-09-22 09:50+07');
SELECT id FROM duty WHERE id=1 FOR UPDATE;
UPDATE execution SET status='running',actual_start='2026-09-22 10:00+07',next_point_no=2,
 review_due_at='2026-09-22 10:35+07' WHERE trip_id=1;
INSERT INTO passage VALUES
 (1,1,'2026-09-22 10:00+07','demo-device-1','00000000-0000-0000-0000-000000000001','Учебная координата у первой точки: 55.03,82.92'),
 (1,2,'2026-09-22 10:16+07','demo-device-1','00000000-0000-0000-0000-000000000002','Учебная координата у второй точки: 55.04,82.93'),
 (1,3,'2026-09-22 10:20+07','demo-device-1','00000000-0000-0000-0000-000000000003','Учебная координата у последней точки: 55.05,82.94');
INSERT INTO deviation(duty_id,trip_id,point_no,kind,detected_at,measured_value)
 VALUES(1,1,2,'late','2026-09-22 10:16+07',6);
UPDATE execution SET status='done',actual_end='2026-09-22 10:20+07',completion_source='telemetry',next_point_no=NULL WHERE trip_id=1;
SELECT trip_id,k,n,round(100.0*k/nullif(n,0),1) AS percent_on_time FROM trip_summary WHERE trip_id=1;
SELECT close_deviation((SELECT id FROM deviation WHERE kind='late' AND trip_id=1),2,'2026-09-22 10:25+07','unreliable_data','Диспетчер проверил журнал: координата второй точки ошибочна');
SELECT trip_id,k,n,unreliable_points,round(100.0*k/nullif(n,0),1) AS percent_on_time FROM trip_summary WHERE trip_id=1;
UPDATE execution SET status='review' WHERE trip_id=2 AND status='waiting' AND review_due_at<'2026-09-22 11:16+07';
SELECT decide_trip(2,2,'2026-09-22 11:17+07','insufficient','Нет ответа водителя');
SELECT decide_trip(2,2,'2026-09-22 11:18+07','continue','Водитель подтвердил движение и начало в 11:00','2026-09-22 11:00+07',NULL,NULL,2);
UPDATE execution SET status='review' WHERE trip_id=2 AND status='running' AND review_due_at<'2026-09-22 11:36+07';
SELECT decide_trip(2,2,'2026-09-22 11:37+07','complete','Водитель подтвердил окончание в 11:25','2026-09-22 11:00+07','2026-09-22 11:25+07');
SELECT close_duty(1,2,'2026-09-22 11:40+07','Смена завершена');
SELECT release_duty(2,2,'2026-09-22 11:50+07');
SELECT decide_trip(3,2,'2026-09-22 12:01+07','fail','Водитель подтвердил отсутствие выезда: поломка');
SELECT close_deviation((SELECT id FROM deviation WHERE trip_id=3 AND kind='failed'),2,'2026-09-22 12:05+07','explained','Поломка подтверждена диспетчером');
SELECT close_duty(2,2,'2026-09-22 12:10+07','Работа завершена после подтверждённого невыполнения');
UPDATE duty SET status='cancelled',closed_at='2026-09-22 09:00+07',closed_by=2,close_reason='Резерв не потребовался' WHERE id=3;
COMMIT;
