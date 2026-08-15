-- Production driver operations: devices, provider callbacks, payout recovery,
-- and durable operational audit records.

CREATE TABLE IF NOT EXISTS user_device_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    platform VARCHAR(20) NOT NULL CHECK (platform IN ('ios','android','web')),
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_device_tokens_active ON user_device_tokens(user_id) WHERE is_active;
DELETE FROM document_expiry_notifications a USING document_expiry_notifications b
WHERE a.ctid < b.ctid AND a.document_id=b.document_id AND a.notification_type=b.notification_type;
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_expiry_notification_once
    ON document_expiry_notifications(document_id, notification_type);

ALTER TABLE driver_background_checks
    ADD COLUMN IF NOT EXISTS provider_reference VARCHAR(255),
    ADD COLUMN IF NOT EXISTS raw_result JSONB;
CREATE UNIQUE INDEX IF NOT EXISTS idx_background_provider_reference
    ON driver_background_checks(provider, provider_reference)
    WHERE provider_reference IS NOT NULL;

CREATE TABLE IF NOT EXISTS background_check_webhook_events (
    provider VARCHAR(50) NOT NULL,
    event_id VARCHAR(255) NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL,
    PRIMARY KEY(provider, event_id)
);

ALTER TABLE driver_payout_transfers
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS reconciled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS provider_status VARCHAR(50),
    ADD COLUMN IF NOT EXISTS wallet_id UUID REFERENCES wallets(id),
    ADD COLUMN IF NOT EXISTS reversal_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS driver_operation_audit (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor_id UUID REFERENCES users(id),
    driver_id UUID REFERENCES users(id),
    operation VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id UUID,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_driver_operation_audit_driver ON driver_operation_audit(driver_id, created_at DESC);

CREATE TABLE IF NOT EXISTS driver_online_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    driver_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    CHECK(ended_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_driver_one_open_session ON driver_online_sessions(driver_id) WHERE ended_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_driver_sessions_history ON driver_online_sessions(driver_id, started_at DESC);
