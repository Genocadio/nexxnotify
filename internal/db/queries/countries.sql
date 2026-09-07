-- name: CreateCountry :one
INSERT INTO countries (code, phone_code, total_digits)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCountryByCode :one
SELECT id, code, phone_code, total_digits, created_at FROM countries WHERE code = $1;

-- name: ListCountries :many
SELECT id, code, phone_code, total_digits, created_at FROM countries ORDER BY code;

-- name: DeleteCountry :execrows
DELETE FROM countries WHERE code = $1;
