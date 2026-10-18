CREATE INDEX events_transaction_receipt_retention ON events(created_at,id) WHERE kind='transaction';
CREATE INDEX spans_receipt_retention ON spans(created_at,id);
