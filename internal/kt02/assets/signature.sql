SELECT c.relname||':'||a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull
      FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid
      WHERE n.nspname='transport' AND c.relkind IN ('r','v') AND a.attnum>0 AND NOT a.attisdropped ORDER BY c.relname,a.attnum;
      SELECT conname||':'||pg_get_constraintdef(oid) FROM pg_constraint WHERE connamespace='transport'::regnamespace ORDER BY conname,pg_get_constraintdef(oid);
      SELECT indexname||':'||indexdef FROM pg_indexes WHERE schemaname='transport' ORDER BY indexname;
      SELECT proname||':'||pg_get_functiondef(oid) FROM pg_proc WHERE pronamespace='transport'::regnamespace ORDER BY proname;
      SELECT c.relname||':'||pg_get_triggerdef(t.oid) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='transport'::regnamespace AND NOT t.tgisinternal ORDER BY c.relname,t.tgname;
      SELECT viewname||':'||definition FROM pg_views WHERE schemaname='transport' ORDER BY viewname;
      SELECT c.relname||':'||coalesce(c.relacl::text,'') FROM pg_class c WHERE c.relnamespace='transport'::regnamespace AND c.relkind IN ('r','v') ORDER BY c.relname;
      SELECT p.proname||':'||pg_get_function_identity_arguments(p.oid)||':'||coalesce(p.proacl::text,'')||':'||p.prosecdef FROM pg_proc p WHERE p.pronamespace='transport'::regnamespace ORDER BY 1;
