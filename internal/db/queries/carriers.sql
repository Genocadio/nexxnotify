-- name: CreateCarrier :one
INSERT INTO carriers (country_id, name, prefixes)
VALUES ($1, $2, sqlc.arg('prefixes')::jsonb)
RETURNING *;

-- name: GetCarrier :one
SELECT c.*, co.code AS country_code
FROM carriers c
JOIN countries co ON co.id = c.country_id
WHERE c.id = $1;

-- name: ListCarriersByCountry :many
SELECT c.*, co.code AS country_code
FROM carriers c
JOIN countries co ON co.id = c.country_id
WHERE c.country_id = $1
ORDER BY c.name;

-- name: DeleteCarrier :execrows
DELETE FROM carriers WHERE id = $1;
