-- name: CreateFlowChannel :one
INSERT INTO flow_channels (flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config)
VALUES ($1, $2, $3, $4, $5, sqlc.narg('template_param_order')::jsonb, sqlc.narg('default_content')::jsonb, sqlc.arg('required_variables')::jsonb, sqlc.arg('channel_config')::jsonb)
RETURNING id, flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config, created_at;

-- name: GetFlowChannel :one
SELECT id, flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config, created_at
FROM flow_channels WHERE id = $1;

-- name: ListFlowChannels :many
SELECT id, flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config, created_at
FROM flow_channels WHERE flow_id = $1 ORDER BY channel;

-- name: UpdateFlowChannel :exec
UPDATE flow_channels
SET enabled = $2, uses_template = $3, template_name = $4, template_param_order = sqlc.narg('template_param_order')::jsonb,
    default_content = sqlc.narg('default_content')::jsonb, required_variables = sqlc.arg('required_variables')::jsonb,
    channel_config = sqlc.arg('channel_config')::jsonb
WHERE id = $1;

-- name: DeleteFlowChannel :execrows
DELETE FROM flow_channels WHERE id = $1;

-- name: UpsertFlowChannel :one
INSERT INTO flow_channels (flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config)
VALUES ($1, $2, $3, $4, $5, sqlc.narg('template_param_order')::jsonb, sqlc.narg('default_content')::jsonb, sqlc.arg('required_variables')::jsonb, sqlc.arg('channel_config')::jsonb)
ON CONFLICT (flow_id, channel) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    uses_template = EXCLUDED.uses_template,
    template_name = EXCLUDED.template_name,
    template_param_order = EXCLUDED.template_param_order,
    default_content = EXCLUDED.default_content,
    required_variables = EXCLUDED.required_variables,
    channel_config = EXCLUDED.channel_config
RETURNING id, flow_id, channel, enabled, uses_template, template_name, template_param_order, default_content, required_variables, channel_config, created_at;
