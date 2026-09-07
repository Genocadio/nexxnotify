package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/nexxserve/nexxnotify/internal/db"
	"github.com/nexxserve/nexxnotify/internal/provider"
)

type Server struct {
	log      *slog.Logger
	registry *provider.Registry
	pool     *pgxpool.Pool
	queries  *db.Queries
}

func NewServer(log *slog.Logger, reg *provider.Registry, pool *pgxpool.Pool, queries *db.Queries) http.Handler {
	s := &Server{log: log, registry: reg, pool: pool, queries: queries}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(limitBody)
	r.Use(middleware.Timeout(20 * time.Second))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealthz)
	r.Get("/providers", s.handleProviders)

	r.Route("/countries", func(r chi.Router) {
		r.Get("/", s.listCountries)
		r.Post("/", s.createCountry)
		r.Get("/{code}", s.getCountry)
		r.Delete("/{code}", s.deleteCountry)
	})

	r.Route("/carriers", func(r chi.Router) {
		r.Get("/", s.listCarriers)
		r.Post("/", s.createCarrier)
		r.Get("/{id}", s.getCarrier)
		r.Delete("/{id}", s.deleteCarrier)
	})

	r.Route("/pricing", func(r chi.Router) {
		r.Get("/", s.listPricing)
		r.Post("/", s.createPricing)
		r.Get("/{id}", s.getPricing)
		r.Delete("/{id}", s.deletePricing)
		r.Post("/{id}/tiers", s.addTier)
		r.Delete("/{id}/tiers/{tierId}", s.deleteTier)
	})

	// Flow management
	r.Route("/flows", func(r chi.Router) {
		r.Get("/", s.listFlows)
		r.Post("/", s.createFlow)
		r.Get("/{id}", s.getFlow)
		r.Put("/{id}", s.updateFlow)
		r.Delete("/{id}", s.deleteFlow)
		r.Post("/{id}/channels", s.upsertFlowChannel)
		r.Delete("/{id}/channels/{channelId}", s.deleteFlowChannel)
	})

	// Send endpoint — triggers a flow with receivers
	r.Post("/send", s.handleSend)

	return r
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

type channelView struct {
	Channel    provider.Channel  `json:"channel"`
	Configured bool              `json:"configured"`
	IsDefault  bool              `json:"is_default"`
	Settings   map[string]string `json:"settings,omitempty"`
}

type providerView struct {
	Name     string        `json:"name"`
	Channels []channelView `json:"channels"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	views := make([]providerView, 0, len(s.registry.Providers()))
	for _, p := range s.registry.Providers() {
		pv := providerView{Name: p.Name(), Channels: []channelView{}}
		for _, cs := range p.Channels() {
			cv := channelView{
				Channel:    cs.Channel,
				Configured: cs.Configured,
				IsDefault:  cs.Configured && s.registry.IsDefault(p.Name(), cs.Channel),
				Settings:   cs.Settings,
			}
			pv.Channels = append(pv.Channels, cv)
		}
		views = append(views, pv)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": views})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
