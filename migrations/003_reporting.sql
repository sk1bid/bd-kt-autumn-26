SET search_path = transport, public;
CREATE INDEX trip_period ON planned_trip(starts_at);
CREATE INDEX trip_point_stop ON trip_point(stop_id);
CREATE INDEX execution_review_due ON execution(review_due_at) WHERE status IN ('waiting','running');
CREATE VIEW passage_assessment AS
 SELECT p.*, coalesce(c.reliable,true) AS reliable,
 extract(epoch FROM (p.arrived_at-t.arrives_at))/60 AS delay_minutes,
 p.arrived_at BETWEEN t.arrives_at-interval '2 minutes' AND t.arrives_at+interval '5 minutes' AS on_time
 FROM passage p JOIN trip_point t USING(trip_id,point_no)
 LEFT JOIN LATERAL (SELECT reliable FROM passage_correction c WHERE c.trip_id=p.trip_id AND c.point_no=p.point_no ORDER BY correction_no DESC LIMIT 1) c ON true;
CREATE VIEW trip_summary AS
 SELECT t.id AS trip_id,t.route_id,t.starts_at,e.status,e.completion_source,
 (e.status='failed' AND e.actual_start IS NOT NULL) AS interrupted,
 (SELECT count(*) FROM trip_point p WHERE p.trip_id=t.id) AS planned_points,
 (SELECT count(*) FROM passage_assessment p WHERE p.trip_id=t.id AND p.reliable AND e.status='done') AS n,
 (SELECT count(*) FROM passage_assessment p WHERE p.trip_id=t.id AND p.reliable AND p.on_time AND e.status='done') AS k,
 (SELECT count(*) FROM passage_assessment p WHERE p.trip_id=t.id AND NOT p.reliable) AS unreliable_points
 FROM planned_trip t LEFT JOIN execution e ON e.trip_id=t.id;
CREATE VIEW duty_plan AS
 SELECT d.id, d.number, d.status,
 sum(last_point.arrives_at-t.starts_at) AS total_planned_duration
 FROM duty d LEFT JOIN assignment a ON a.duty_id=d.id AND a.ended_at IS NULL
 LEFT JOIN planned_trip t ON t.id=a.trip_id
 LEFT JOIN LATERAL (SELECT max(arrives_at) arrives_at FROM trip_point WHERE trip_id=t.id) last_point ON true
 GROUP BY d.id;
