SELECT jsonb_build_object('pid',a.pid,'application',a.application_name,'state',a.state,
 'wait_event_type',a.wait_event_type,'wait_event',a.wait_event,
 'blocking_pids',pg_blocking_pids(a.pid),'query',a.query,
 'locks',(SELECT jsonb_agg(jsonb_build_object('locktype',l.locktype,'mode',l.mode,'granted',l.granted,
  'relation',l.relation::regclass::text,'transactionid',l.transactionid)) FROM pg_locks l WHERE l.pid=a.pid))
FROM pg_stat_activity a
WHERE a.datname=current_database() AND a.application_name IN ('kt03_first','kt03_second')
ORDER BY a.application_name
