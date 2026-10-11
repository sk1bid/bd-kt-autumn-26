SELECT d.id AS device_id,d.vendor,count(*) AS total,
 count(*) FILTER (WHERE k.result='accepted') AS accepted,
 count(*) FILTER (WHERE k.result='invalid') AS invalid,
 count(*) FILTER (WHERE k.result='stale') AS stale,
 round(100.0*count(*) FILTER (WHERE k.result<>'accepted')/count(*),2) AS rejected_percent
FROM transport.message_key k JOIN transport.device d ON d.id=k.device_id
WHERE k.first_received_at>=:'period_start'::timestamptz AND k.first_received_at<:'period_end'::timestamptz
GROUP BY d.id,d.vendor ORDER BY total DESC,d.id
