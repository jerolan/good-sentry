ALTER TABLE alert_deliveries DROP CONSTRAINT alert_deliveries_recipient_type_check;
ALTER TABLE alert_deliveries ADD CONSTRAINT alert_deliveries_recipient_type_check
 CHECK (recipient_type IN ('email','webhook','slack'));
