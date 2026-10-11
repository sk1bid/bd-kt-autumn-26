CREATE INDEX IF NOT EXISTS message_device_received
ON transport.message_key(device_id,first_received_at DESC,message_id DESC);
ANALYZE transport.message_key;
