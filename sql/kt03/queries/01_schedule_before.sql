SELECT r.id AS route_id,r.number,
 count(*) AS planned_trips,
 count(*) FILTER (WHERE s.status='done') AS done_trips,
 count(*) FILTER (WHERE s.status='failed') AS failed_trips,
 count(*) FILTER (WHERE s.status='review') AS review_trips,
 count(*) FILTER (WHERE s.status IN ('waiting','running')) AS pending_trips,
 count(*) FILTER (WHERE s.status IS NULL) AS unreleased_trips,
 sum(s.planned_points) AS planned_points,sum(s.k) AS k,sum(s.n) AS n,
 round(100.0*sum(s.k)/nullif(sum(s.n),0),1) AS percent_on_time,
 sum(s.unreliable_points) AS unreliable_points
FROM transport.trip_summary s JOIN transport.route r ON r.id=s.route_id
WHERE s.starts_at>=:'period_start'::timestamptz AND s.starts_at<:'period_end'::timestamptz
GROUP BY r.id,r.number ORDER BY r.id
