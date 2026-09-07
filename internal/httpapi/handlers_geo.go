package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/nexxserve/nexxnotify/internal/db"
)

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)
var phoneCodeRe = regexp.MustCompile(`^[0-9]{1,5}$`)
var prefixRe = regexp.MustCompile(`^[0-9]{1,7}$`)

type countryView struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	PhoneCode   string `json:"phone_code"`
	TotalDigits int32  `json:"total_digits"`
}

type createCountryReq struct {
	Code        string `json:"code"`
	PhoneCode   string `json:"phone_code"`
	TotalDigits int32  `json:"total_digits"`
}

func (s *Server) createCountry(w http.ResponseWriter, r *http.Request) {
	var req createCountryReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	req.PhoneCode = strings.TrimPrefix(strings.TrimSpace(req.PhoneCode), "+")

	if !countryRe.MatchString(req.Code) {
		errJSON(w, http.StatusBadRequest, "code must be a 2-letter ISO country code")
		return
	}
	if !phoneCodeRe.MatchString(req.PhoneCode) {
		errJSON(w, http.StatusBadRequest, "phone_code must be 1-5 digits")
		return
	}
	if req.TotalDigits <= 0 {
		errJSON(w, http.StatusBadRequest, "total_digits must be > 0")
		return
	}

	country, err := s.queries.CreateCountry(r.Context(), db.CreateCountryParams{
		Code:        req.Code,
		PhoneCode:   req.PhoneCode,
		TotalDigits: req.TotalDigits,
	})
	if err != nil {
		if isUniqueViolation(err) {
			errJSON(w, http.StatusConflict, "country "+req.Code+" already exists")
			return
		}
		s.log.Error("create country", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toCountryView(country))
}

func (s *Server) getCountry(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(chi.URLParam(r, "code"))
	country, err := s.queries.GetCountryByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "country not found")
			return
		}
		s.log.Error("get country", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toCountryView(country))
}

func (s *Server) listCountries(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListCountries(r.Context())
	if err != nil {
		s.log.Error("list countries", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]countryView, 0, len(rows))
	for _, c := range rows {
		out = append(out, toCountryView(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"countries": out})
}

func (s *Server) deleteCountry(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(chi.URLParam(r, "code"))
	n, err := s.queries.DeleteCountry(r.Context(), code)
	if err != nil {
		if isFKViolation(err) {
			errJSON(w, http.StatusConflict, "country is referenced by carriers or pricing")
			return
		}
		s.log.Error("delete country", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "country not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toCountryView(c db.Country) countryView {
	return countryView{
		ID:          c.ID.String(),
		Code:        c.Code,
		PhoneCode:   c.PhoneCode,
		TotalDigits: c.TotalDigits,
	}
}

type carrierView struct {
	ID          string   `json:"id"`
	CountryCode string   `json:"country_code"`
	Name        string   `json:"name"`
	Prefixes    []string `json:"prefixes"`
}

type createCarrierReq struct {
	CountryCode string   `json:"country_code"`
	Name        string   `json:"name"`
	Prefixes    []string `json:"prefixes"`
}

func (s *Server) createCarrier(w http.ResponseWriter, r *http.Request) {
	var req createCarrierReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.CountryCode = strings.ToUpper(strings.TrimSpace(req.CountryCode))
	req.Name = strings.TrimSpace(req.Name)

	if !countryRe.MatchString(req.CountryCode) {
		errJSON(w, http.StatusBadRequest, "country_code must be a 2-letter ISO country code")
		return
	}
	if req.Name == "" {
		errJSON(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Prefixes) == 0 {
		errJSON(w, http.StatusBadRequest, "at least one prefix is required")
		return
	}
	for i, p := range req.Prefixes {
		if !prefixRe.MatchString(strings.TrimSpace(p)) {
			errJSON(w, http.StatusBadRequest, "prefix "+strconv.Itoa(i)+" must be 1-7 digits")
			return
		}
		req.Prefixes[i] = strings.TrimSpace(p)
	}

	country, err := s.queries.GetCountryByCode(r.Context(), req.CountryCode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "unknown country "+req.CountryCode)
			return
		}
		s.log.Error("resolve country", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	prefixes, _ := json.Marshal(req.Prefixes)
	carrier, err := s.queries.CreateCarrier(r.Context(), db.CreateCarrierParams{
		CountryID: country.ID,
		Name:      req.Name,
		Prefixes:  prefixes,
	})
	if err != nil {
		if isUniqueViolation(err) {
			errJSON(w, http.StatusConflict, "carrier already exists for this country")
			return
		}
		s.log.Error("create carrier", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, carrierToView(carrier.ID, carrier.Name, carrier.Prefixes, country.Code))
}

func (s *Server) getCarrier(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid carrier id")
		return
	}
	row, err := s.queries.GetCarrier(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "carrier not found")
			return
		}
		s.log.Error("get carrier", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, carrierToView(row.ID, row.Name, row.Prefixes, row.CountryCode))
}

func (s *Server) listCarriers(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country")))
	if code == "" {
		errJSON(w, http.StatusBadRequest, "country query parameter is required")
		return
	}
	country, err := s.queries.GetCountryByCode(r.Context(), code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "unknown country "+code)
			return
		}
		s.log.Error("resolve country", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	rows, err := s.queries.ListCarriersByCountry(r.Context(), country.ID)
	if err != nil {
		s.log.Error("list carriers", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]carrierView, 0, len(rows))
	for _, row := range rows {
		out = append(out, carrierToView(row.ID, row.Name, row.Prefixes, row.CountryCode))
	}
	writeJSON(w, http.StatusOK, map[string]any{"carriers": out})
}

func (s *Server) deleteCarrier(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid carrier id")
		return
	}
	n, err := s.queries.DeleteCarrier(r.Context(), id)
	if err != nil {
		if isFKViolation(err) {
			errJSON(w, http.StatusConflict, "carrier is referenced by pricing")
			return
		}
		s.log.Error("delete carrier", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "carrier not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func carrierToView(id uuid.UUID, name string, prefixes []byte, countryCode string) carrierView {
	var list []string
	_ = json.Unmarshal(prefixes, &list)
	if list == nil {
		list = []string{}
	}
	return carrierView{
		ID:          id.String(),
		CountryCode: countryCode,
		Name:        name,
		Prefixes:    list,
	}
}
