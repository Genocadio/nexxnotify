package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/nexxserve/nexxnotify/internal/db"
)

var validPricingChannels = map[string]bool{"sms": true, "whatsapp": true, "email": true}
var validResetPeriods = map[string]bool{
	"MONTHLY": true, "QUARTERLY": true, "YEARLY": true, "LIFETIME": true, "CUSTOM": true,
}
var tierPriceRe = regexp.MustCompile(`^\d+(\.\d{1,6})?$`)

type tierView struct {
	ID        string `json:"id"`
	MinVolume int64  `json:"min_volume"`
	MaxVolume int64  `json:"max_volume"`
	TierPrice string `json:"tier_price"`
}

type pricingView struct {
	ID                uuid.UUID  `json:"id"`
	Provider          string     `json:"provider"`
	Channel           string     `json:"channel"`
	CountryCode       *string    `json:"country_code"`
	CarrierName       *string    `json:"carrier_name"`
	VolumeResetPeriod string     `json:"volume_reset_period"`
	VolumeResetDays   *int32     `json:"volume_reset_days"`
	VolumeResetAnchor *string    `json:"volume_reset_anchor"`
	Tiers             []tierView `json:"tiers,omitempty"`
}

type tierInput struct {
	MinVolume int64  `json:"min_volume"`
	MaxVolume int64  `json:"max_volume"`
	TierPrice string `json:"tier_price"`
}

type createPricingReq struct {
	Provider          string      `json:"provider"`
	Channel           string      `json:"channel"`
	CountryCode       string      `json:"country_code"`
	CarrierID         string      `json:"carrier_id"`
	VolumeResetPeriod string      `json:"volume_reset_period"`
	VolumeResetDays   *int32      `json:"volume_reset_days"`
	VolumeResetAnchor string      `json:"volume_reset_anchor"`
	Tiers             []tierInput `json:"tiers"`
}

func (s *Server) createPricing(w http.ResponseWriter, r *http.Request) {
	var req createPricingReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Channel = strings.ToLower(strings.TrimSpace(req.Channel))
	period := strings.ToUpper(strings.TrimSpace(req.VolumeResetPeriod))
	if period == "" {
		period = "MONTHLY"
	}

	if _, err := s.queries.GetProvider(r.Context(), req.Provider); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusBadRequest, "unknown provider "+req.Provider)
			return
		}
		s.log.Error("resolve provider", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !validPricingChannels[req.Channel] {
		errJSON(w, http.StatusBadRequest, "channel must be one of sms, whatsapp, email")
		return
	}
	if !validResetPeriods[period] {
		errJSON(w, http.StatusBadRequest, "volume_reset_period must be one of MONTHLY, QUARTERLY, YEARLY, LIFETIME, CUSTOM")
		return
	}

	var resetDays pgtype.Int4
	var resetAnchor pgtype.Date
	if period == "CUSTOM" {
		if req.VolumeResetDays == nil || *req.VolumeResetDays <= 0 {
			errJSON(w, http.StatusBadRequest, "volume_reset_days is required and must be > 0 for CUSTOM period")
			return
		}
		resetDays = pgtype.Int4{Int32: *req.VolumeResetDays, Valid: true}
		if req.VolumeResetAnchor != "" {
			t, err := time.Parse("2006-01-02", req.VolumeResetAnchor)
			if err != nil {
				errJSON(w, http.StatusBadRequest, "volume_reset_anchor must be a date formatted YYYY-MM-DD")
				return
			}
			resetAnchor = pgtype.Date{Time: t.UTC(), Valid: true}
		}
	} else if req.VolumeResetDays != nil || req.VolumeResetAnchor != "" {
		errJSON(w, http.StatusBadRequest, "volume_reset_days/anchor are only allowed for CUSTOM period")
		return
	}

	var countryID *uuid.UUID
	var carrierID *uuid.UUID
	countryCode := strings.ToUpper(strings.TrimSpace(req.CountryCode))

	if req.Channel == "email" {
		if countryCode != "" || req.CarrierID != "" {
			errJSON(w, http.StatusBadRequest, "email pricing has no country/carrier data; omit country_code and carrier_id")
			return
		}
	} else {
		if countryCode == "" {
			errJSON(w, http.StatusBadRequest, "country_code is required for "+req.Channel+" pricing")
			return
		}
		country, err := s.queries.GetCountryByCode(r.Context(), countryCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				errJSON(w, http.StatusNotFound, "unknown country "+countryCode)
				return
			}
			s.log.Error("resolve country", "error", err)
			errJSON(w, http.StatusInternalServerError, "internal error")
			return
		}
		countryID = &country.ID

		if req.CarrierID != "" {
			cid, err := uuid.Parse(req.CarrierID)
			if err != nil {
				errJSON(w, http.StatusBadRequest, "carrier_id must be a UUID")
				return
			}
			carrier, err := s.queries.GetCarrier(r.Context(), cid)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					errJSON(w, http.StatusNotFound, "unknown carrier")
					return
				}
				s.log.Error("resolve carrier", "error", err)
				errJSON(w, http.StatusInternalServerError, "internal error")
				return
			}
			if carrier.CountryID != country.ID {
				errJSON(w, http.StatusBadRequest, "carrier does not belong to country "+countryCode)
				return
			}
			carrierID = &cid
		}
	}

	tierViews := make([]tierView, 0, len(req.Tiers))
	if len(req.Tiers) > 0 {
		if msg := validateTierRanges(req.Tiers); msg != "" {
			errJSON(w, http.StatusBadRequest, msg)
			return
		}
	}

	row, err := s.queries.CreatePricing(r.Context(), db.CreatePricingParams{
		ProviderID:        req.Provider,
		Channel:           req.Channel,
		CountryID:         countryID,
		CarrierID:         carrierID,
		VolumeResetPeriod: period,
		VolumeResetDays:   resetDays,
		VolumeResetAnchor: resetAnchor,
	})
	if err != nil {
		if isUniqueViolation(err) {
			errJSON(w, http.StatusConflict, "pricing rule already exists for this provider/channel/country/carrier")
			return
		}
		s.log.Error("create pricing", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	if len(req.Tiers) > 0 {
		tx, err := s.pool.Begin(r.Context())
		if err != nil {
			s.log.Error("begin tiers txn", "error", err)
			errJSON(w, http.StatusInternalServerError, "internal error")
			return
		}
		defer tx.Rollback(r.Context())
		tq := s.queries.WithTx(tx)
		for _, t := range req.Tiers {
			tier, err := tq.CreateTier(r.Context(), db.CreateTierParams{
				PricingID: row.ID,
				MinVolume: t.MinVolume,
				MaxVolume: t.MaxVolume,
				TierPrice: t.TierPrice,
			})
			if err != nil {
				s.log.Error("create tier", "error", err)
				errJSON(w, http.StatusInternalServerError, "internal error")
				return
			}
			tierViews = append(tierViews, toTierView(tier))
		}
		if err := tx.Commit(r.Context()); err != nil {
			s.log.Error("commit tiers txn", "error", err)
			errJSON(w, http.StatusInternalServerError, "internal error")
			return
		}
		sort.Slice(tierViews, func(i, j int) bool { return tierViews[i].MinVolume < tierViews[j].MinVolume })
	}

	fresh, err := s.queries.GetPricing(r.Context(), row.ID)
	if err != nil {
		s.log.Error("reload pricing", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, pricingGetRowToView(fresh, tierViews))
}

func (s *Server) getPricing(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid pricing id")
		return
	}
	row, err := s.queries.GetPricing(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "pricing rule not found")
			return
		}
		s.log.Error("get pricing", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	tiers, err := s.queries.ListTiersByPricing(r.Context(), id)
	if err != nil {
		s.log.Error("list tiers", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	tierViews := make([]tierView, 0, len(tiers))
	for _, t := range tiers {
		tierViews = append(tierViews, toTierView(t))
	}
	writeJSON(w, http.StatusOK, pricingGetRowToView(row, tierViews))
}

func (s *Server) listPricing(w http.ResponseWriter, r *http.Request) {
	params := db.ListPricingParams{}
	if v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider"))); v != "" {
		params.ProviderID = pgtype.Text{String: v, Valid: true}
	}
	if v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("channel"))); v != "" {
		params.Channel = pgtype.Text{String: v, Valid: true}
	}
	if v := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("country"))); v != "" {
		params.CountryCode = pgtype.Text{String: v, Valid: true}
	}
	rows, err := s.queries.ListPricing(r.Context(), params)
	if err != nil {
		s.log.Error("list pricing", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]pricingView, 0, len(rows))
	for _, row := range rows {
		out = append(out, pricingListRowToView(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pricing": out})
}

func (s *Server) addTier(w http.ResponseWriter, r *http.Request) {
	pricingID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid pricing id")
		return
	}
	var in tierInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := validateTierRanges([]tierInput{in}); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}
	existing, err := s.queries.ListTiersByPricing(r.Context(), pricingID)
	if err != nil {
		s.log.Error("list tiers", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, t := range existing {
		if in.MinVolume <= t.MaxVolume && t.MinVolume <= in.MaxVolume {
			errJSON(w, http.StatusConflict, "tier overlaps existing range ["+itoa(t.MinVolume)+","+itoa(t.MaxVolume)+"]")
			return
		}
	}
	tier, err := s.queries.CreateTier(r.Context(), db.CreateTierParams{
		PricingID: pricingID,
		MinVolume: in.MinVolume,
		MaxVolume: in.MaxVolume,
		TierPrice: in.TierPrice,
	})
	if err != nil {
		if isFKViolation(err) {
			errJSON(w, http.StatusNotFound, "pricing rule not found")
			return
		}
		s.log.Error("create tier", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toTierView(tier))
}

func (s *Server) deleteTier(w http.ResponseWriter, r *http.Request) {
	pricingID, err1 := uuid.Parse(chi.URLParam(r, "id"))
	tierID, err2 := uuid.Parse(chi.URLParam(r, "tierId"))
	if err1 != nil || err2 != nil {
		errJSON(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := s.queries.DeleteTier(r.Context(), db.DeleteTierParams{ID: tierID, PricingID: pricingID})
	if err != nil {
		s.log.Error("delete tier", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "tier not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePricing(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid pricing id")
		return
	}
	n, err := s.queries.DeletePricing(r.Context(), id)
	if err != nil {
		s.log.Error("delete pricing", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "pricing rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateTierRanges(tiers []tierInput) string {
	sorted := make([]tierInput, len(tiers))
	copy(sorted, tiers)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MinVolume < sorted[j].MinVolume })
	for i, t := range sorted {
		if t.MinVolume < 0 || t.MaxVolume <= t.MinVolume {
			return "tier " + strconv.Itoa(i) + ": need 0 <= min_volume < max_volume"
		}
		if !tierPriceRe.MatchString(strings.TrimSpace(t.TierPrice)) {
			return "tier " + strconv.Itoa(i) + ": tier_price must be a decimal with up to 6 places"
		}
		if i > 0 && sorted[i-1].MaxVolume >= t.MinVolume {
			return "tier " + strconv.Itoa(i) + ": volume ranges must not overlap"
		}
	}
	return ""
}

func toTierView(t db.ProviderCountryPricingTier) tierView {
	return tierView{ID: t.ID.String(), MinVolume: t.MinVolume, MaxVolume: t.MaxVolume, TierPrice: t.TierPrice}
}

func nullableText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func nullableInt4(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

func nullableDate(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	v := d.Time.Format("2006-01-02")
	return &v
}

func pricingGetRowToView(row db.GetPricingRow, tiers []tierView) pricingView {
	return pricingView{
		ID:                row.ID,
		Provider:          row.ProviderID,
		Channel:           row.Channel,
		CountryCode:       nullableText(row.CountryCode),
		CarrierName:       nullableText(row.CarrierName),
		VolumeResetPeriod: row.VolumeResetPeriod,
		VolumeResetDays:   nullableInt4(row.VolumeResetDays),
		VolumeResetAnchor: nullableDate(row.VolumeResetAnchor),
		Tiers:             tiers,
	}
}

func pricingListRowToView(row db.ListPricingRow) pricingView {
	return pricingView{
		ID:                row.ID,
		Provider:          row.ProviderID,
		Channel:           row.Channel,
		CountryCode:       nullableText(row.CountryCode),
		CarrierName:       nullableText(row.CarrierName),
		VolumeResetPeriod: row.VolumeResetPeriod,
		VolumeResetDays:   nullableInt4(row.VolumeResetDays),
		VolumeResetAnchor: nullableDate(row.VolumeResetAnchor),
	}
}
