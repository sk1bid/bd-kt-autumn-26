SELECT jsonb_build_object(
 'telemetry_start_outside_window',(
  SELECT count(*) FROM execution e JOIN planned_trip t ON t.id=e.trip_id
  LEFT JOIN passage p ON p.trip_id=e.trip_id AND p.point_no=1
  WHERE (e.completion_source='telemetry' OR e.status='running')
   AND (e.actual_start<t.starts_at-interval '2 minutes'
     OR e.actual_start>t.starts_at+interval '15 minutes'
     OR e.actual_start IS DISTINCT FROM p.arrived_at)),
 'telemetry_after_unresolved_review',(
  SELECT count(*) FROM execution e JOIN planned_trip t ON t.id=e.trip_id
  JOIN assignment a ON a.trip_id=e.trip_id AND a.ended_at IS NULL
  WHERE (e.completion_source='telemetry' OR e.status='running') AND EXISTS (
   SELECT 1 FROM assignment b JOIN execution previous ON previous.trip_id=b.trip_id
   JOIN planned_trip p ON p.id=b.trip_id
   WHERE b.duty_id=a.duty_id AND b.ended_at IS NULL
    AND previous.status='review' AND p.starts_at<t.starts_at
  ))
);
