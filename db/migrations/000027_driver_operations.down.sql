DROP TABLE IF EXISTS driver_operation_audit;
DROP TABLE IF EXISTS driver_online_sessions;
ALTER TABLE driver_payout_transfers DROP COLUMN IF EXISTS reversal_at, DROP COLUMN IF EXISTS wallet_id, DROP COLUMN IF EXISTS provider_status, DROP COLUMN IF EXISTS reconciled_at, DROP COLUMN IF EXISTS next_retry_at, DROP COLUMN IF EXISTS attempt_count;
DROP TABLE IF EXISTS background_check_webhook_events;
DROP INDEX IF EXISTS idx_background_provider_reference;
ALTER TABLE driver_background_checks DROP COLUMN IF EXISTS raw_result, DROP COLUMN IF EXISTS provider_reference;
DROP TABLE IF EXISTS user_device_tokens;
DROP INDEX IF EXISTS idx_document_expiry_notification_once;
