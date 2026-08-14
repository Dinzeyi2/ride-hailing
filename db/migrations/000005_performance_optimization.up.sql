-- Performance Optimization Migration
-- This migration fixes payment schema and adds portable PostgreSQL indexes.
-- Live driver proximity is provided by Redis; application migrations must not
-- require server extensions that Railway's managed PostgreSQL does not ship.

-- 2. Fix payments table schema to match repository/model
-- Add missing Stripe-related columns
ALTER TABLE payments ADD COLUMN IF NOT EXISTS payment_method VARCHAR(20);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS stripe_payment_id VARCHAR(255);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS stripe_charge_id VARCHAR(255);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS currency VARCHAR(3) DEFAULT 'USD';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS metadata JSONB;

-- Make commission and driver_earnings nullable (calculated fields)
ALTER TABLE payments ALTER COLUMN commission DROP NOT NULL;
ALTER TABLE payments ALTER COLUMN driver_earnings DROP NOT NULL;

-- Update existing records to set payment_method from method column
UPDATE payments SET payment_method = method WHERE payment_method IS NULL;

-- 3. Add is_active column to wallets (for better queries)
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT true;

-- 4. Create optimized indexes for performance

-- Payments indexes
CREATE INDEX IF NOT EXISTS idx_payments_rider_id_created_at ON payments(rider_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_driver_id_created_at ON payments(driver_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status) WHERE status IN ('pending', 'failed');
CREATE INDEX IF NOT EXISTS idx_payments_stripe_payment_id ON payments(stripe_payment_id) WHERE stripe_payment_id IS NOT NULL;

-- Rides indexes for common queries
CREATE INDEX IF NOT EXISTS idx_rides_rider_id_status ON rides(rider_id, status);
CREATE INDEX IF NOT EXISTS idx_rides_driver_id_status ON rides(driver_id, status) WHERE driver_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_rides_status_created_at ON rides(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rides_completed_at ON rides(completed_at DESC) WHERE completed_at IS NOT NULL;

-- Wallet transactions indexes
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_wallet_created ON wallet_transactions(wallet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_wallet_transactions_reference ON wallet_transactions(reference_type, reference_id) WHERE reference_id IS NOT NULL;

-- Driver location history index (nearby-driver matching is handled by Redis)
CREATE INDEX IF NOT EXISTS idx_driver_locations_driver_time ON driver_locations(driver_id, recorded_at DESC);

-- User indexes for faster lookups
CREATE INDEX IF NOT EXISTS idx_users_role_active ON users(role, is_active) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_users_email_lower ON users(LOWER(email));

-- 5. Add materialized view for driver statistics (cache heavy computations)
CREATE MATERIALIZED VIEW IF NOT EXISTS driver_statistics AS
SELECT
    d.id AS driver_id,
    d.user_id,
    d.rating,
    d.total_rides,
    COUNT(DISTINCT r.id) AS completed_rides_count,
    COALESCE(SUM(p.driver_earnings), 0) AS total_earnings,
    COALESCE(AVG(r.rating), 0) AS average_rating,
    MAX(r.completed_at) AS last_ride_completed
FROM drivers d
LEFT JOIN rides r ON r.driver_id = d.user_id AND r.status = 'completed'
LEFT JOIN payments p ON p.ride_id = r.id
GROUP BY d.id, d.user_id, d.rating, d.total_rides;

-- Create index on materialized view
CREATE UNIQUE INDEX IF NOT EXISTS idx_driver_stats_driver_id ON driver_statistics(driver_id);

-- 6. Add refresh function for materialized view
CREATE OR REPLACE FUNCTION refresh_driver_statistics()
RETURNS void AS $$
BEGIN
    REFRESH MATERIALIZED VIEW CONCURRENTLY driver_statistics;
END;
$$ LANGUAGE plpgsql;

-- Optional query-monitoring extensions are infrastructure concerns and are not
-- installed by application migrations on managed PostgreSQL.

-- 10. Add comment documentation
COMMENT ON MATERIALIZED VIEW driver_statistics IS 'Cached driver performance statistics - refresh periodically';
COMMENT ON FUNCTION refresh_driver_statistics() IS 'Refresh the driver_statistics materialized view concurrently';
