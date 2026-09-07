-- name: CreatePricing :one
INSERT INTO provider_country_pricing
    (provider_id, channel, country_id, carrier_id, volume_reset_period, volume_reset_days, volume_reset_anchor)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPricing :one
SELECT p.*,
       c.code  AS country_code,
       cr.name AS carrier_name
FROM provider_country_pricing p
LEFT JOIN countries c ON c.id = p.country_id
LEFT JOIN carriers cr ON cr.id = p.carrier_id
WHERE p.id = $1;

-- name: ListPricing :many
SELECT p.*,
       c.code  AS country_code,
       cr.name AS carrier_name
FROM provider_country_pricing p
LEFT JOIN countries c ON c.id = p.country_id
LEFT JOIN carriers cr ON cr.id = p.carrier_id
WHERE (sqlc.narg('provider_id')::text IS NULL OR p.provider_id = sqlc.narg('provider_id'))
  AND (sqlc.narg('channel')::text IS NULL OR p.channel = sqlc.narg('channel'))
  AND (sqlc.narg('country_code')::char(2) IS NULL OR c.code = sqlc.narg('country_code'))
ORDER BY p.provider_id, p.channel, c.code NULLS FIRST;

-- name: DeletePricing :execrows
DELETE FROM provider_country_pricing WHERE id = $1;

-- name: CreateTier :one
INSERT INTO provider_country_pricing_tiers (pricing_id, min_volume, max_volume, tier_price)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListTiersByPricing :many
SELECT id, pricing_id, min_volume, max_volume, tier_price, created_at
FROM provider_country_pricing_tiers
WHERE pricing_id = $1
ORDER BY min_volume;

-- name: DeleteTier :execrows
DELETE FROM provider_country_pricing_tiers WHERE id = $1 AND pricing_id = $2;
