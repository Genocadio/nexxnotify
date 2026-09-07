-- name: CreateFlow :one
INSERT INTO flows (id, name, active, input_contract)
VALUES ($1, $2, $3, sqlc.arg('input_contract')::jsonb)
RETURNING id, name, active, input_contract, created_at, updated_at;

-- name: GetFlow :one
SELECT id, name, active, input_contract, created_at, updated_at
FROM flows WHERE id = $1;

-- name: ListFlows :many
SELECT id, name, active, input_contract, created_at, updated_at
FROM flows ORDER BY created_at DESC;

-- name: UpdateFlow :exec
UPDATE flows
SET name = $2, active = $3, input_contract = sqlc.arg('input_contract')::jsonb, updated_at = now()
WHERE id = $1;

-- name: DeleteFlow :execrows
DELETE FROM flows WHERE id = $1;
