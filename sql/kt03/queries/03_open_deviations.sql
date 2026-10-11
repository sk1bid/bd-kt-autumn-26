SELECT d.id,d.detected_at,d.kind,v.registration_no,n.number AS duty_number,
 r.number AS route_number,d.trip_id,d.point_no,d.measured_value
FROM transport.deviation d JOIN transport.duty n ON n.id=d.duty_id
JOIN transport.vehicle v ON v.id=n.vehicle_id
LEFT JOIN transport.planned_trip t ON t.id=d.trip_id LEFT JOIN transport.route r ON r.id=t.route_id
WHERE d.closed_at IS NULL AND d.detected_at>=:'period_start'::timestamptz
 AND d.detected_at<:'period_end'::timestamptz
ORDER BY d.detected_at DESC,d.id DESC LIMIT :'limit'::integer
