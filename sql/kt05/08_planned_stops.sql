-- Сколько остановок диспетчеру предстоит контролировать на маршруте
-- с начала выбранного периода до отправления очередного рейса включительно?
WITH per_trip AS (
    SELECT t.route_id, t.id, t.starts_at, count(p.point_no) AS planned_stops
    FROM transport.planned_trip t
    LEFT JOIN transport.trip_point p ON p.trip_id = t.id
    WHERE t.starts_at >= :'period_start' AND t.starts_at < :'period_end'
    GROUP BY t.route_id, t.id, t.starts_at
)
SELECT *, sum(planned_stops) OVER (PARTITION BY route_id ORDER BY starts_at, id
                                   ROWS UNBOUNDED PRECEDING) AS cumulative_stops
FROM per_trip ORDER BY route_id, starts_at, id;
