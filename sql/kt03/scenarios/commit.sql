BEGIN;
SET LOCAL search_path=transport,public;
SELECT replace_duty(1,3,2,'2026-09-22 09:55+07','Подмена исправным автобусом','03000000-0000-4000-8000-000000000001');
COMMIT;
DO $$ BEGIN
 IF (SELECT status FROM duty WHERE id=1)<>'closed'
 OR (SELECT status FROM duty WHERE id=3)<>'on_line'
 OR (SELECT count(*) FROM assignment WHERE duty_id=1 AND ended_at IS NOT NULL)<>2
 OR (SELECT count(*) FROM assignment WHERE duty_id=3 AND ended_at IS NULL)<>2
 OR (SELECT count(*) FROM execution)<>2
 OR (SELECT count(*) FROM operation_request)<>1 THEN
  RAISE EXCEPTION 'Committed replacement is incomplete';
 END IF;
END $$;
