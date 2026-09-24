SET search_path=transport,public;
DO $$ BEGIN
 IF (SELECT count(*) FROM duty WHERE status='closed')<>2 OR (SELECT count(*) FROM duty WHERE status='cancelled')<>1 THEN RAISE EXCEPTION 'Duty results'; END IF;
 IF (SELECT count(*) FROM execution WHERE status='done')<>2 OR (SELECT count(*) FROM execution WHERE status='failed')<>1 THEN RAISE EXCEPTION 'Execution results'; END IF;
 IF (SELECT count(*) FROM trip_decision WHERE trip_id=2)<>3 THEN RAISE EXCEPTION 'Decision history'; END IF;
 IF (SELECT count(*) FROM passage WHERE trip_id=2)<>0 THEN RAISE EXCEPTION 'Manual completion invented passages'; END IF;
 IF (SELECT count(*) FROM passage_correction)<>1 THEN RAISE EXCEPTION 'Correction history'; END IF;
 IF (SELECT count(*) FROM deviation WHERE closed_at IS NULL)<>0 THEN RAISE EXCEPTION 'Deviation results'; END IF;
 IF (SELECT review_due_at FROM trip_decision WHERE trip_id=2 AND decision_no=2)<>'2026-09-22 11:35+07'::timestamptz THEN RAISE EXCEPTION 'Next review deadline'; END IF;
 BEGIN UPDATE trip_decision SET reason='Rewrite history' WHERE trip_id=2 AND decision_no=1;
  RAISE EXCEPTION 'History mutable'; EXCEPTION WHEN check_violation THEN NULL; END;
 BEGIN UPDATE duty SET status='on_line',closed_at=NULL,closed_by=NULL,close_reason=NULL WHERE id=1;
  RAISE EXCEPTION 'Closed duty reopened'; EXCEPTION WHEN check_violation THEN NULL; END;
END $$;
