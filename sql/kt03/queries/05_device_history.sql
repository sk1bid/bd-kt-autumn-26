SELECT k.device_id,k.message_id,k.first_received_at,k.generated_at,k.result,
 v.registration_no
FROM transport.message_key k
LEFT JOIN transport.device_installation i ON i.id=k.installation_id
LEFT JOIN transport.vehicle v ON v.id=i.vehicle_id
WHERE k.device_id=:'device_id' AND k.first_received_at>=:'period_start'::timestamptz
 AND k.first_received_at<:'period_end'::timestamptz
ORDER BY k.first_received_at DESC,k.message_id DESC LIMIT :'limit'::integer
