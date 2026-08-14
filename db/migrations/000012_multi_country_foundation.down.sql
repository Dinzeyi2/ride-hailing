-- Multi-Country Foundation Migration Rollback
-- Reverses all changes from 000012_multi_country_foundation.up.sql

-- ========================================
-- DROP TRIGGERS
-- ========================================

DROP TRIGGER IF EXISTS update_country_payment_methods_updated_at ON country_payment_methods;
DROP TRIGGER IF EXISTS update_time_multipliers_updated_at ON time_multipliers;
DROP TRIGGER IF EXISTS update_zone_fees_updated_at ON zone_fees;
DROP TRIGGER IF EXISTS update_pricing_configs_updated_at ON pricing_configs;
DROP TRIGGER IF EXISTS update_pricing_config_versions_updated_at ON pricing_config_versions;
DROP TRIGGER IF EXISTS update_pricing_zones_updated_at ON pricing_zones;
DROP TRIGGER IF EXISTS update_cities_updated_at ON cities;
DROP TRIGGER IF EXISTS update_regions_updated_at ON regions;
DROP TRIGGER IF EXISTS update_countries_updated_at ON countries;

-- ========================================
-- DROP INDEXES ON EXTENDED TABLES
-- ========================================

DROP INDEX IF EXISTS idx_rides_city;
DROP INDEX IF EXISTS idx_rides_country;
DROP INDEX IF EXISTS idx_users_country;

-- ========================================
-- REMOVE COLUMNS FROM EXISTING TABLES
-- ========================================

-- Remove columns from rides
ALTER TABLE rides DROP COLUMN IF EXISTS negotiation_session_id;
ALTER TABLE rides DROP COLUMN IF EXISTS was_negotiated;
ALTER TABLE rides DROP COLUMN IF EXISTS pricing_version_id;
ALTER TABLE rides DROP COLUMN IF EXISTS currency_code;
ALTER TABLE rides DROP COLUMN IF EXISTS dropoff_zone_id;
ALTER TABLE rides DROP COLUMN IF EXISTS pickup_zone_id;
ALTER TABLE rides DROP COLUMN IF EXISTS city_id;
ALTER TABLE rides DROP COLUMN IF EXISTS region_id;
ALTER TABLE rides DROP COLUMN IF EXISTS country_id;

-- Remove columns from payments
ALTER TABLE payments DROP COLUMN IF EXISTS tax_rate_pct;
ALTER TABLE payments DROP COLUMN IF EXISTS tax_amount;
ALTER TABLE payments DROP COLUMN IF EXISTS country_payment_method_id;
ALTER TABLE payments DROP COLUMN IF EXISTS exchange_rate_id;
ALTER TABLE payments DROP COLUMN IF EXISTS exchange_rate;
ALTER TABLE payments DROP COLUMN IF EXISTS settlement_amount;
ALTER TABLE payments DROP COLUMN IF EXISTS settlement_currency;
ALTER TABLE payments DROP COLUMN IF EXISTS original_amount;
ALTER TABLE payments DROP COLUMN IF EXISTS original_currency;
ALTER TABLE payments DROP COLUMN IF EXISTS region_id;

-- Remove columns from users
ALTER TABLE users DROP COLUMN IF EXISTS preferred_currency;
ALTER TABLE users DROP COLUMN IF EXISTS preferred_language;
ALTER TABLE users DROP COLUMN IF EXISTS home_region_id;
ALTER TABLE users DROP COLUMN IF EXISTS country_id;

-- ========================================
-- DROP NEW TABLES (reverse order of creation)
-- ========================================

DROP TABLE IF EXISTS driver_regions;
DROP TABLE IF EXISTS pricing_history;
DROP TABLE IF EXISTS pricing_audit_logs;
DROP TABLE IF EXISTS country_payment_methods;
DROP TABLE IF EXISTS surge_thresholds;
DROP TABLE IF EXISTS event_multipliers;
DROP TABLE IF EXISTS weather_multipliers;
DROP TABLE IF EXISTS time_multipliers;
DROP TABLE IF EXISTS zone_fees;
DROP TABLE IF EXISTS pricing_configs;
DROP TABLE IF EXISTS pricing_config_versions;
DROP TABLE IF EXISTS pricing_zones;
DROP TABLE IF EXISTS cities;
DROP TABLE IF EXISTS regions;
DROP TABLE IF EXISTS countries;
DROP TABLE IF EXISTS exchange_rates;
DROP TABLE IF EXISTS currencies;
