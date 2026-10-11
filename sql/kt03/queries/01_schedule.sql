WITH selected AS MATERIALIZED (
 SELECT t.id,t.route_id,e.status
 FROM transport.planned_trip t LEFT JOIN transport.execution e ON e.trip_id=t.id
 WHERE t.starts_at>=:'period_start'::timestamptz AND t.starts_at<:'period_end'::timestamptz
), points AS (
 SELECT p.trip_id,count(*) AS planned_points,
 count(a.trip_id) FILTER (WHERE coalesce(c.reliable,true) AND s.status='done') AS n,
 count(a.trip_id) FILTER (WHERE coalesce(c.reliable,true) AND s.status='done'
  AND a.arrived_at BETWEEN p.arrives_at-interval '2 minutes' AND p.arrives_at+interval '5 minutes') AS k,
 count(a.trip_id) FILTER (WHERE NOT coalesce(c.reliable,true)) AS unreliable_points
 FROM selected s JOIN transport.trip_point p ON p.trip_id=s.id
 LEFT JOIN transport.passage a ON (a.trip_id,a.point_no)=(p.trip_id,p.point_no)
 LEFT JOIN LATERAL (SELECT reliable FROM transport.passage_correction c
  WHERE c.trip_id=p.trip_id AND c.point_no=p.point_no ORDER BY correction_no DESC LIMIT 1) c ON true
 GROUP BY p.trip_id
)
SELECT r.id AS route_id,r.number,
 count(*) AS planned_trips,
 count(*) FILTER (WHERE s.status='done') AS done_trips,
 count(*) FILTER (WHERE s.status='failed') AS failed_trips,
 count(*) FILTER (WHERE s.status='review') AS review_trips,
 count(*) FILTER (WHERE s.status IN ('waiting','running')) AS pending_trips,
 count(*) FILTER (WHERE s.status IS NULL) AS unreleased_trips,
 sum(p.planned_points) AS planned_points,sum(p.k) AS k,sum(p.n) AS n,
 round(100.0*sum(p.k)/nullif(sum(p.n),0),1) AS percent_on_time,
 sum(p.unreliable_points) AS unreliable_points
FROM selected s JOIN transport.route r ON r.id=s.route_id JOIN points p ON p.trip_id=s.id
GROUP BY r.id,r.number ORDER BY r.id
