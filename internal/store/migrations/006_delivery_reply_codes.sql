-- Preserve parsed three-digit replies even when the receiver uses an
-- unregistered status. Classification belongs to the delivery transport.
ALTER TABLE alert_deliveries DROP CONSTRAINT alert_deliveries_last_status_code_check;
ALTER TABLE alert_deliveries ADD CONSTRAINT alert_deliveries_last_status_code_check
 CHECK (last_status_code BETWEEN 100 AND 999);
ALTER TABLE alert_attempts DROP CONSTRAINT alert_attempts_status_code_check;
ALTER TABLE alert_attempts ADD CONSTRAINT alert_attempts_status_code_check
 CHECK (status_code BETWEEN 100 AND 999);
