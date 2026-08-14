-- Rollback Performance Optimization Migration

-- Drop views and functions
DROP FUNCTION IF EXISTS get_nearby_drivers_postgis(DOUBLE PRECISION, DOUBLE PRECISION, DOUBLE PRECISION);
DROP FUNCTION IF EXISTS refresh_driver_statistics();
DROP MATERIALIZED VIEW IF EXISTS driver_statistics;

-- Drop performance indexes
DROP INDEX IF EXISTS idx_users_email_lower;
DROP INDEX IF EXISTS idx_users_role_active;
DROP INDEX IF EXISTS idx_drivers_location;
DROP INDEX IF EXISTS idx_driver_locations_driver_time;
DROP INDEX IF EXISTS idx_driver_locations_spatial;
DROP INDEX IF EXISTS idx_wallet_transactions_reference;
DROP INDEX IF EXISTS idx_wallet_transactions_wallet_created;
DROP INDEX IF EXISTS idx_rides_completed_at;
DROP INDEX IF EXISTS idx_rides_status_created_at;
DROP INDEX IF EXISTS idx_rides_driver_id_status;
DROP INDEX IF EXISTS idx_rides_rider_id_status;
DROP INDEX IF EXISTS idx_payments_stripe_payment_id;
DROP INDEX IF EXISTS idx_payments_status;
DROP INDEX IF EXISTS idx_payments_driver_id_created_at;
DROP INDEX IF EXISTS idx_payments_rider_id_created_at;

-- Remove is_active column from wallets
ALTER TABLE wallets DROP COLUMN IF EXISTS is_active;

-- Revert payment table changes (note: metadata and new columns will be dropped)
ALTER TABLE payments DROP COLUMN IF EXISTS metadata;
ALTER TABLE payments DROP COLUMN IF EXISTS currency;
ALTER TABLE payments DROP COLUMN IF EXISTS stripe_charge_id;
ALTER TABLE payments DROP COLUMN IF EXISTS stripe_payment_id;
ALTER TABLE payments DROP COLUMN IF EXISTS payment_method;

-- Make commission and driver_earnings NOT NULL again
ALTER TABLE payments ALTER COLUMN commission SET NOT NULL;
ALTER TABLE payments ALTER COLUMN driver_earnings SET NOT NULL;

-- Drop extensions (only if no other objects depend on them)
DROP EXTENSION IF EXISTS postgis CASCADE;
