-- Какие маршруты дают диспетчеру больше всего плановых рейсов за период?
WITH loads AS (
    SELECT r.id, r.number, count(*) AS trips
    FROM transport.planned_trip t JOIN transport.route r ON r.id = t.route_id
    WHERE t.starts_at >= :'period_start' AND t.starts_at < :'period_end'
    GROUP BY r.id, r.number
)
SELECT *, dense_rank() OVER (ORDER BY trips DESC) AS load_rank
FROM loads ORDER BY load_rank, id;
