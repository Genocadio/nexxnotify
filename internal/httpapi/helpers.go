package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func errJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			errJSON(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		errJSON(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func isUniqueViolation(err error) bool { return pgErrorCode(err) == "23505" }

func isFKViolation(err error) bool { return pgErrorCode(err) == "23503" }
