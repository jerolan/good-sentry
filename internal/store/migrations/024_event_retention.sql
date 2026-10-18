CREATE INDEX events_error_receipt_retention ON events(created_at,id) WHERE kind<>'transaction';
