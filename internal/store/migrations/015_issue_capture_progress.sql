-- Historical group totals can change when original partitions move. Mute
-- progress and first-capture activity need a counter that never moves with them.
ALTER TABLE issues ADD COLUMN receipt_count bigint NOT NULL DEFAULT 0 CHECK (receipt_count >= 0);
UPDATE issues SET receipt_count=count;
