-- Fare Keep Rate: progressive weekly driver commission and transparent rider fees.

CREATE TABLE commission_tiers (
    min_ride INTEGER PRIMARY KEY,
    max_ride INTEGER,
    commission_rate DECIMAL(5,4) NOT NULL CHECK (commission_rate >= 0 AND commission_rate <= 1),
    driver_keep_rate DECIMAL(5,4) NOT NULL CHECK (driver_keep_rate >= 0 AND driver_keep_rate <= 1),
    CHECK (max_ride IS NULL OR max_ride >= min_ride)
);

INSERT INTO commission_tiers (min_ride, max_ride, commission_rate, driver_keep_rate) VALUES
    (1,  20, 0.20, 0.80),
    (21, 40, 0.17, 0.83),
    (41, 60, 0.14, 0.86),
    (61, 80, 0.10, 0.90),
    (81, NULL, 0.07, 0.93);

CREATE TABLE driver_weekly_rewards (
    driver_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,
    completed_rides INTEGER NOT NULL DEFAULT 0 CHECK (completed_rides >= 0),
    current_commission_rate DECIMAL(5,4) NOT NULL DEFAULT 0.20,
    current_keep_rate DECIMAL(5,4) NOT NULL DEFAULT 0.80,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (driver_id, week_start)
);

CREATE TABLE ride_commission_snapshots (
    ride_id UUID PRIMARY KEY REFERENCES rides(id) ON DELETE CASCADE,
    driver_id UUID NOT NULL REFERENCES users(id),
    week_start DATE NOT NULL,
    weekly_ride_number INTEGER NOT NULL,
    trip_value DECIMAL(12,2) NOT NULL,
    service_fee DECIMAL(12,2) NOT NULL,
    government_fees DECIMAL(12,2) NOT NULL DEFAULT 0,
    rider_total DECIMAL(12,2) NOT NULL,
    commission_rate DECIMAL(5,4) NOT NULL,
    commission_amount DECIMAL(12,2) NOT NULL,
    driver_keep_rate DECIMAL(5,4) NOT NULL,
    driver_payout DECIMAL(12,2) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_driver_weekly_rewards_week ON driver_weekly_rewards(week_start, completed_rides DESC);
CREATE INDEX idx_ride_commission_driver_week ON ride_commission_snapshots(driver_id, week_start, weekly_ride_number);

ALTER TABLE payments ADD COLUMN IF NOT EXISTS trip_value DECIMAL(12,2);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS service_fee DECIMAL(12,2) NOT NULL DEFAULT 0;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS government_fees DECIMAL(12,2) NOT NULL DEFAULT 0;
ALTER TABLE payments ADD COLUMN IF NOT EXISTS commission_rate DECIMAL(5,4);
ALTER TABLE payments ADD COLUMN IF NOT EXISTS driver_keep_rate DECIMAL(5,4);

UPDATE payments
SET trip_value = COALESCE(trip_value, amount),
    commission_rate = COALESCE(commission_rate,
        CASE WHEN amount > 0 AND commission IS NOT NULL THEN commission / amount ELSE 0.20 END),
    driver_keep_rate = COALESCE(driver_keep_rate,
        CASE WHEN amount > 0 AND driver_earnings IS NOT NULL THEN driver_earnings / amount ELSE 0.80 END)
WHERE trip_value IS NULL OR commission_rate IS NULL OR driver_keep_rate IS NULL;

CREATE OR REPLACE FUNCTION fare_reward_tier(ride_number INTEGER)
RETURNS TABLE (commission_rate DECIMAL(5,4), keep_rate DECIMAL(5,4), next_threshold INTEGER)
LANGUAGE SQL STABLE AS $$
    SELECT t.commission_rate, t.driver_keep_rate,
           (SELECT MIN(n.min_ride) FROM commission_tiers n WHERE n.min_ride > ride_number)
    FROM commission_tiers t
    WHERE ride_number >= t.min_ride AND (t.max_ride IS NULL OR ride_number <= t.max_ride)
    LIMIT 1
$$;

CREATE OR REPLACE FUNCTION track_driver_keep_rate()
RETURNS TRIGGER AS $$
DECLARE
    week_date DATE;
    ride_number INTEGER;
    tier_commission DECIMAL(5,4);
    tier_keep DECIMAL(5,4);
    next_ride_commission DECIMAL(5,4);
    next_ride_keep DECIMAL(5,4);
    next_min INTEGER;
    rides_remaining INTEGER;
    trip_amount DECIMAL(12,2);
    booking_fee DECIMAL(12,2);
BEGIN
    IF NEW.status <> 'completed' OR NEW.driver_id IS NULL OR
       (TG_OP = 'UPDATE' AND OLD.status = 'completed') THEN
        RETURN NEW;
    END IF;

    week_date := (date_trunc('week', COALESCE(NEW.completed_at, NOW()) AT TIME ZONE 'UTC'))::date;

    INSERT INTO driver_weekly_rewards (driver_id, week_start, completed_rides)
    VALUES (NEW.driver_id, week_date, 1)
    ON CONFLICT (driver_id, week_start) DO UPDATE
      SET completed_rides = driver_weekly_rewards.completed_rides + 1,
          updated_at = NOW()
    RETURNING completed_rides INTO ride_number;

    SELECT commission_rate, keep_rate, next_threshold
      INTO tier_commission, tier_keep, next_min
      FROM fare_reward_tier(ride_number);

    SELECT commission_rate, keep_rate
      INTO next_ride_commission, next_ride_keep
      FROM fare_reward_tier(ride_number + 1);

    UPDATE driver_weekly_rewards
       SET current_commission_rate = next_ride_commission,
           current_keep_rate = next_ride_keep,
           updated_at = NOW()
     WHERE driver_id = NEW.driver_id AND week_start = week_date;

    trip_amount := ROUND(COALESCE(NEW.final_fare, NEW.estimated_fare, 0)::numeric, 2);
    booking_fee := ROUND(GREATEST(2.49, LEAST(7.00, trip_amount * 0.10))::numeric, 2);

    INSERT INTO ride_commission_snapshots (
        ride_id, driver_id, week_start, weekly_ride_number, trip_value,
        service_fee, government_fees, rider_total, commission_rate,
        commission_amount, driver_keep_rate, driver_payout
    ) VALUES (
        NEW.id, NEW.driver_id, week_date, ride_number, trip_amount,
        booking_fee, 0, trip_amount + booking_fee, tier_commission,
        ROUND(trip_amount * tier_commission, 2), tier_keep,
        ROUND(trip_amount * tier_keep, 2)
    ) ON CONFLICT (ride_id) DO NOTHING;

    -- Record the driver's earned balance immediately. The unique partial index
    -- added in migration 22 makes this safe if an event consumer retries it.
    INSERT INTO driver_earnings (
        id, driver_id, ride_id, type, gross_amount, commission, net_amount,
        currency, description, is_paid_out, created_at
    ) VALUES (
        uuid_generate_v4(), NEW.driver_id, NEW.id, 'ride_fare', trip_amount,
        ROUND(trip_amount * tier_commission, 2), ROUND(trip_amount * tier_keep, 2),
        COALESCE(NEW.currency_code, 'USD'),
        'Ride fare (' || ROUND(tier_commission * 100) || '% Fare commission)', false, NOW()
    ) ON CONFLICT (ride_id, type) WHERE ride_id IS NOT NULL DO NOTHING;

    SELECT max_ride INTO next_min
      FROM commission_tiers
     WHERE ride_number >= min_ride AND (max_ride IS NULL OR ride_number <= max_ride);

    IF next_min IS NOT NULL THEN
        rides_remaining := next_min - ride_number;
        IF rides_remaining IN (10, 7, 5, 3, 1) THEN
            INSERT INTO notifications (id, user_id, type, channel, title, body, data, created_at)
            VALUES (
                uuid_generate_v4(), NEW.driver_id, 'keep_rate_progress', 'push',
                'Your next keep rate is close',
                rides_remaining || ' more rides until you keep ' ||
                    ROUND((SELECT driver_keep_rate FROM commission_tiers WHERE min_ride = next_min + 1) * 100) || '%',
                jsonb_build_object('rides_remaining', rides_remaining,
                    'next_keep_rate', (SELECT driver_keep_rate FROM commission_tiers WHERE min_ride = next_min + 1),
                    'weekly_rides', ride_number, 'action', 'keep_rate_progress'),
                NOW()
            );
        END IF;
    END IF;

    IF ride_number IN (20, 40, 60, 80) THEN
        INSERT INTO notifications (id, user_id, type, channel, title, body, data, created_at)
        VALUES (
            uuid_generate_v4(), NEW.driver_id, 'keep_rate_unlocked', 'push',
            'New keep rate unlocked',
            'You now keep ' || ROUND(next_ride_keep * 100) || '% of every eligible trip value for your next rides this week',
            jsonb_build_object('keep_rate', next_ride_keep, 'commission_rate', next_ride_commission,
                'weekly_rides', ride_number, 'action', 'keep_rate_unlocked'),
            NOW()
        );
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_track_driver_keep_rate
AFTER INSERT OR UPDATE OF status ON rides
FOR EACH ROW EXECUTE FUNCTION track_driver_keep_rate();
