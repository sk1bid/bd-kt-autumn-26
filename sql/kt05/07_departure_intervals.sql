-- Интервал между соседними отправлениями одного маршрута внутри периода.
SELECT r.number, t.id, t.starts_at,
       t.starts_at - lag(t.starts_at) OVER
         (PARTITION BY r.id ORDER BY t.starts_at, t.id) AS departure_gap
FROM transport.planned_trip t JOIN transport.route r ON r.id = t.route_id
WHERE t.starts_at >= :'period_start' AND t.starts_at < :'period_end'
ORDER BY r.id, t.starts_at, t.id;
