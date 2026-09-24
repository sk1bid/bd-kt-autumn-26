SELECT json_build_object('tables',(SELECT json_agg(x) FROM (
 SELECT c.relname AS name,
 (SELECT json_agg(y) FROM (SELECT a.attname AS name,format_type(a.atttypid,a.atttypmod) AS type,
 a.attnotnull AS required,a.attidentity AS identity,pg_get_expr(d.adbin,d.adrelid) AS default_value
 FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
 WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum) y) AS columns,
 (SELECT json_agg(y) FROM (SELECT conname AS name,contype AS kind,pg_get_constraintdef(oid) AS definition FROM pg_constraint WHERE conrelid=c.oid ORDER BY conname) y) AS constraints
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='transport' AND c.relkind='r' ORDER BY c.relname) x));