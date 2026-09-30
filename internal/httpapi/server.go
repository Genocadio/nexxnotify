package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexxserve/nexxnotify/internal/auth"
	db "github.com/nexxserve/nexxnotify/internal/db"
	"github.com/nexxserve/nexxnotify/internal/provider"
)

type Server struct {
	log         *slog.Logger
	registry    *provider.Registry
	pool        *pgxpool.Pool
	queries     *db.Queries
	apiKey      string          // fallback static API key (if configured)
	verifier    *auth.Verifier  // cryptographic token verifier with replay protection
	allowedNets []*net.IPNet    // if non-empty, requests must originate from one of these CIDRs
}

func NewServer(log *slog.Logger, reg *provider.Registry, pool *pgxpool.Pool, queries *db.Queries, apiKey string, publicKey string, allowedCIDRs []string) http.Handler {
	s := &Server{
		log:      log,
		registry: reg,
		pool:     pool,
		queries:  queries,
		apiKey:   apiKey,
	}

	// Initialize cryptographic verifier (Public key or HMAC secret)
	keyToUse := strings.TrimSpace(publicKey)
	if keyToUse == "" {
		keyToUse = strings.TrimSpace(apiKey)
	}
	if keyToUse != "" {
		v, err := auth.NewVerifier(keyToUse)
		if err != nil {
			log.Error("failed to initialize auth token verifier", "err", err)
		} else {
			s.verifier = v
			log.Info("cryptographic auth verifier initialized (single-use replay prevention enabled)")
		}
	}
	for _, cidr := range allowedCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		// Support plain IPs without mask — treat as /32 or /128.
		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				log.Warn("invalid IP in NEXXNOTIFY_ALLOWED_IPS, skipping", "value", cidr)
				continue
			}
			if ip.To4() != nil {
				cidr += "/32"
			} else {
				cidr += "/128"
			}
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Warn("invalid CIDR in NEXXNOTIFY_ALLOWED_IPS, skipping", "value", cidr, "err", err)
			continue
		}
		s.allowedNets = append(s.allowedNets, ipNet)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(limitBody)
	r.Use(middleware.Timeout(20 * time.Second))
	r.Use(middleware.Recoverer)

	// /healthz is always public — used by load-balancers, nexxauth health checks, etc.
	r.Get("/healthz", s.handleHealthz)

	// All other routes require a valid API key when one is configured.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAllowedIP) // IP check first — cheapest rejection
		r.Use(s.requireAuth)

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
	})

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

// requireAllowedIP blocks requests whose source IP is not in s.allowedNets.
// When allowedNets is empty the middleware is a no-op (open mode).
// chi's RealIP middleware must run first so r.RemoteAddr is already the
// real client IP (not the proxy's).
func (s *Server) requireAllowedIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.allowedNets) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr // no port, use as-is
		}
		ip := net.ParseIP(host)
		if ip == nil {
			s.log.Warn("rejected request: could not parse remote IP",
				"remote_addr", r.RemoteAddr, "path", r.URL.Path)
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "forbidden: source address not allowed",
			})
			return
		}
		for _, allowed := range s.allowedNets {
			if allowed.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		s.log.Warn("rejected request: IP not in allowlist",
			"remote_ip", ip.String(), "path", r.URL.Path)
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "forbidden: source address not allowed",
		})
	})
}

// requireAuth enforces authentication on protected routes.
// It prioritizes cryptographically signed single-use JWTs (Bearer token),
// ensuring zero replay attacks. It also supports static X-Api-Key as a fallback.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Open mode (dev): neither verifier nor api key is configured
		if s.verifier == nil && s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Check for signed JWT token in Authorization: Bearer <token> or X-Nexxauth-Token
		token := ""
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}
		if token == "" {
			token = strings.TrimSpace(r.Header.Get("X-Nexxauth-Token"))
		}

		if token != "" && s.verifier != nil {
			claims, err := s.verifier.VerifyToken(token)
			if err != nil {
				s.log.Warn("rejected request: invalid auth token",
					"err", err,
					"method", r.Method,
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
				)
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": fmt.Sprintf("unauthorized: %v", err),
				})
				return
			}
			s.log.Debug("request authenticated via signed token",
				"jti", claims.JTI,
				"path", r.URL.Path,
			)
			next.ServeHTTP(w, r)
			return
		}

		// 2. Fallback: static X-Api-Key check
		if s.apiKey != "" {
			provided := r.Header.Get("X-Api-Key")
			if subtleEqual(provided, s.apiKey) {
				next.ServeHTTP(w, r)
				return
			}
		}

		s.log.Warn("rejected request: missing or invalid credentials",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized: valid Bearer token or X-Api-Key required",
		})
	})
}

// subtleEqual compares two strings in constant time to prevent timing attacks.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
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
