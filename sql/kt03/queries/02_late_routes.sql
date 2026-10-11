SELECT r.id AS route_id,r.number,count(*) AS late_points,
 count(DISTINCT t.id) AS affected_trips,round(avg(p.delay_minutes),2) AS mean_delay_minutes
FROM transport.deviation d JOIN transport.planned_trip t ON t.id=d.trip_id
JOIN transport.route r ON r.id=t.route_id
JOIN transport.passage_assessment p ON (p.trip_id,p.point_no)=(d.trip_id,d.point_no)
WHERE d.kind='late' AND p.reliable
 AND t.starts_at>=:'period_start'::timestamptz AND t.starts_at<:'period_end'::timestamptz
GROUP BY r.id,r.number
HAVING count(*)>=:'min_late'::integer
ORDER BY late_points DESC,r.id
