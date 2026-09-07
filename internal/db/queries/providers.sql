-- name: SeedProvider :one
INSERT INTO providers (id, display_name)
VALUES ($1, $2)
ON CONFLICT (id) DO UPDATE SET display_name = EXCLUDED.display_name
RETURNING id;

-- name: GetProvider :one
SELECT id, display_name, created_at FROM providers WHERE id = $1;

-- name: ListProviders :many
SELECT id, display_name, created_at FROM providers ORDER BY id;
