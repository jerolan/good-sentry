ALTER TABLE projects
 ADD COLUMN event_throttle_rate integer NOT NULL DEFAULT 0
 CHECK (event_throttle_rate BETWEEN 0 AND 100);
