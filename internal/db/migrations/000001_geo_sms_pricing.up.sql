CREATE TABLE providers (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE countries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code         CHAR(2) NOT NULL UNIQUE,
    phone_code   TEXT NOT NULL,
    total_digits INTEGER NOT NULL CHECK (total_digits > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE carriers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    country_id UUID NOT NULL REFERENCES countries(id) ON DELETE RESTRICT,
    name       TEXT NOT NULL,
    prefixes   JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (country_id, name)
);

CREATE TABLE provider_country_pricing (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id          TEXT NOT NULL REFERENCES providers(id) ON DELETE RESTRICT,
    channel              TEXT NOT NULL CHECK (channel IN ('sms', 'whatsapp', 'email')),
    country_id           UUID REFERENCES countries(id) ON DELETE RESTRICT,
    carrier_id           UUID REFERENCES carriers(id) ON DELETE RESTRICT,
    volume_reset_period  TEXT NOT NULL DEFAULT 'MONTHLY'
        CHECK (volume_reset_period IN ('MONTHLY', 'QUARTERLY', 'YEARLY', 'LIFETIME', 'CUSTOM')),
    volume_reset_days    INTEGER CHECK (volume_reset_days > 0),
    volume_reset_anchor  DATE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (volume_reset_period = 'CUSTOM' AND volume_reset_days IS NOT NULL)
        OR (volume_reset_period <> 'CUSTOM' AND volume_reset_days IS NULL AND volume_reset_anchor IS NULL)
    ),
    UNIQUE NULLS NOT DISTINCT (provider_id, channel, country_id, carrier_id)
);

CREATE INDEX idx_pricing_country ON provider_country_pricing(country_id);
CREATE INDEX idx_carriers_country ON carriers(country_id);

CREATE TABLE provider_country_pricing_tiers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pricing_id  UUID NOT NULL REFERENCES provider_country_pricing(id) ON DELETE CASCADE,
    min_volume  BIGINT NOT NULL CHECK (min_volume >= 0),
    max_volume  BIGINT NOT NULL CHECK (max_volume > min_volume),
    tier_price  NUMERIC(10,6) NOT NULL CHECK (tier_price > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tiers_pricing ON provider_country_pricing_tiers(pricing_id);
