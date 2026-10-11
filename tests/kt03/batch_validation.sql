BEGIN;
SET LOCAL search_path=transport,public;
INSERT INTO app_user(login,full_name) VALUES ('batch-test','Batch test');
INSERT INTO user_role SELECT id,'analyst' FROM app_user WHERE login='batch-test';
UPDATE app_user SET full_name='Batch checked' WHERE login='batch-test';
DO $$ BEGIN
 IF (SELECT count(*) FROM transport_private.model_check_queue)<>1 THEN RAISE EXCEPTION 'Expected one pending check'; END IF;
END $$;
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM transport_private.model_check_queue) THEN RAISE EXCEPTION 'Queue not drained'; END IF;
 BEGIN
  DELETE FROM user_role WHERE user_id=(SELECT id FROM app_user WHERE login='batch-test');
  RAISE EXCEPTION 'Immediate mode missed invalid change';
 EXCEPTION WHEN check_violation THEN
  IF SQLERRM NOT LIKE '%Every user needs at least one role%' THEN RAISE; END IF;
 END;
END $$;
SET CONSTRAINTS ALL DEFERRED;
SAVEPOINT before_invalid;
DELETE FROM user_role WHERE user_id=(SELECT id FROM app_user WHERE login='batch-test');
ROLLBACK TO before_invalid;
SET CONSTRAINTS ALL IMMEDIATE;
SET CONSTRAINTS ALL DEFERRED;
DO $$ BEGIN
 BEGIN
  DELETE FROM user_role WHERE user_id=(SELECT id FROM app_user WHERE login='batch-test');
  SET CONSTRAINTS ALL IMMEDIATE;
  RAISE EXCEPTION 'Deferred mode missed invalid change';
 EXCEPTION WHEN check_violation THEN
  IF SQLERRM NOT LIKE '%Every user needs at least one role%' THEN RAISE; END IF;
 END;
END $$;
SET CONSTRAINTS ALL IMMEDIATE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM transport_private.model_check_queue) THEN RAISE EXCEPTION 'Savepoint leaked queue entry'; END IF;
END $$;
SET LOCAL ROLE transport_app;
DO $$ BEGIN
 BEGIN
  DELETE FROM transport_private.model_check_queue;
  RAISE EXCEPTION 'Application can bypass validation';
 EXCEPTION WHEN insufficient_privilege THEN NULL;
 END;
END $$;
RESET ROLE;
ROLLBACK;
SELECT 'PASS batching, immediate/deferred, savepoints, rollback, queue permissions';
