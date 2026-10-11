BEGIN;
SET LOCAL search_path=transport,public;
\set ON_ERROR_STOP off
SELECT replace_duty(3,2,2,'2026-09-22 09:56+07','Отказ из-за допуска','03000000-0000-4000-8000-000000000002');
\set failure_state :SQLSTATE
\set ON_ERROR_STOP on
ROLLBACK;
SELECT :'failure_state'='23514' AS expected_error \gset
\if :expected_error
\echo PASS controlled ROLLBACK SQLSTATE :failure_state
\else
\quit 1
\endif
