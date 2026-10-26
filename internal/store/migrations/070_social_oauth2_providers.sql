ALTER TABLE social_apps DROP CONSTRAINT IF EXISTS social_apps_provider_check;
ALTER TABLE social_apps ADD CONSTRAINT social_apps_provider_check
 CHECK (provider IN ('openid_connect','google','github','gitlab'));
