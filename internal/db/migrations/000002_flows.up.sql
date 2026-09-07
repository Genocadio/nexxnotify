-- A Flow is a reusable messaging configuration with an input contract
-- and channel composition. Flows are global (not tenant-scoped) and
-- define which channels are active and how each channel renders content.
CREATE TABLE flows (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    active          BOOLEAN NOT NULL DEFAULT true,
    input_contract  JSONB NOT NULL DEFAULT '{"allows_content": false, "allows_variables": false}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Each flow_channel belongs to a flow and describes how one channel
-- (sms, email, fcm, whatsapp) renders content for that flow.
-- Stores either template config or default content, plus required variables.
CREATE TABLE flow_channels (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id             TEXT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    channel             TEXT NOT NULL CHECK (channel IN ('sms', 'email', 'fcm', 'whatsapp')),
    enabled             BOOLEAN NOT NULL DEFAULT true,
    uses_template       BOOLEAN NOT NULL DEFAULT false,
    template_name       TEXT,
    template_param_order JSONB,
    default_content     JSONB,
    required_variables  JSONB NOT NULL DEFAULT '[]',
    channel_config      JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (flow_id, channel)
);

CREATE INDEX idx_flow_channels_flow_id ON flow_channels(flow_id);
