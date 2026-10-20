-- Expand the network probe kinds without rewriting monitor state or history.
ALTER TABLE monitors DROP CONSTRAINT monitors_kind_check;
ALTER TABLE monitors ADD CONSTRAINT monitors_kind_check
 CHECK (kind IN ('HEAD','GET','POST','TCP','SSL','heartbeat'));
