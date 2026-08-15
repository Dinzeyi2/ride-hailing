CREATE TABLE IF NOT EXISTS ride_offers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ride_id UUID NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
    driver_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'offered'
        CHECK (status IN ('offered', 'accepted', 'expired', 'withdrawn', 'rejected')),
    score DECIMAL(8,5),
    distance_km DECIMAL(10,3),
    offered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '30 seconds',
    responded_at TIMESTAMPTZ,
    UNIQUE (ride_id, driver_id)
);

CREATE INDEX IF NOT EXISTS idx_ride_offers_driver_active
    ON ride_offers(driver_id, expires_at DESC) WHERE status = 'offered';
CREATE INDEX IF NOT EXISTS idx_ride_offers_ride ON ride_offers(ride_id, status);

CREATE TABLE IF NOT EXISTS driver_stripe_accounts (
    driver_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    stripe_account_id VARCHAR(255) NOT NULL UNIQUE,
    details_submitted BOOLEAN NOT NULL DEFAULT false,
    charges_enabled BOOLEAN NOT NULL DEFAULT false,
    payouts_enabled BOOLEAN NOT NULL DEFAULT false,
    disabled_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS driver_payout_transfers (
    id UUID PRIMARY KEY,
    driver_id UUID NOT NULL REFERENCES users(id),
    stripe_account_id VARCHAR(255) NOT NULL,
    stripe_transfer_id VARCHAR(255) UNIQUE,
    amount DECIMAL(12,2) NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(20) NOT NULL CHECK (status IN ('processing','paid','failed')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_driver_payout_transfers_driver ON driver_payout_transfers(driver_id, created_at DESC);
