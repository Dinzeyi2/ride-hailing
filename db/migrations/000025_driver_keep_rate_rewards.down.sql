-- Roll back Fare Keep Rate pricing, progress tracking, and payment fields.
DROP TRIGGER IF EXISTS trg_track_driver_keep_rate ON rides;
DROP FUNCTION IF EXISTS track_driver_keep_rate();
DROP FUNCTION IF EXISTS fare_reward_tier(INTEGER);
DROP TABLE IF EXISTS ride_commission_snapshots;
DROP TABLE IF EXISTS driver_weekly_rewards;
DROP TABLE IF EXISTS commission_tiers;
ALTER TABLE payments DROP COLUMN IF EXISTS driver_keep_rate;
ALTER TABLE payments DROP COLUMN IF EXISTS commission_rate;
ALTER TABLE payments DROP COLUMN IF EXISTS government_fees;
ALTER TABLE payments DROP COLUMN IF EXISTS service_fee;
ALTER TABLE payments DROP COLUMN IF EXISTS trip_value;
