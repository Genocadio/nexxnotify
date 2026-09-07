package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joho/godotenv"

	"github.com/nexxserve/nexxnotify/internal/config"
	db "github.com/nexxserve/nexxnotify/internal/db"
	"github.com/nexxserve/nexxnotify/internal/httpapi"
	"github.com/nexxserve/nexxnotify/internal/provider"
)

var providerDisplayNames = map[string]string{
	"smtp":    "SMTP",
	"resend":  "Resend",
	"ses":     "Amazon SES",
	"twilio":  "Twilio",
	"infobip": "Infobip",
	"fcm":     "Firebase Cloud Messaging",
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	_ = godotenv.Load()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	reg, err := buildRegistry(cfg)
	if err != nil {
		return err
	}

	logProviders(logger, reg)

	ctx := context.Background()
	pool, err := connectPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("database connected", "url_masked", maskDatabaseURL(cfg.DatabaseURL))

	if err := db.MigrateUp(cfg.DatabaseURL); err != nil {
		return err
	}
	logger.Info("migrations applied")

	if err := seedProviders(ctx, pool, reg); err != nil {
		return err
	}
	logger.Info("providers seeded")

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewServer(logger, reg, pool, db.New(pool)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func connectPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			return nil, fmt.Errorf("parse database URL: %w", err)
		}
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("create pool: %w", err)
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		pool.Close()
		lastErr = err
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return nil, fmt.Errorf("database unreachable after retries: %w", lastErr)
}

func seedProviders(ctx context.Context, pool *pgxpool.Pool, reg *provider.Registry) error {
	q := db.New(pool)
	for _, p := range reg.Providers() {
		displayName, ok := providerDisplayNames[p.Name()]
		if !ok {
			displayName = strings.Title(p.Name())
		}
		if _, err := q.SeedProvider(ctx, db.SeedProviderParams{ID: p.Name(), DisplayName: displayName}); err != nil {
			return fmt.Errorf("seed provider %s: %w", p.Name(), err)
		}
	}
	return nil
}

func maskDatabaseURL(url string) string {
	if i := strings.Index(url, "@"); i > 0 {
		schemeEnd := strings.Index(url, "://")
		if schemeEnd > 0 && strings.Contains(url[schemeEnd+3:i], ":") {
			return url[:schemeEnd+3] + "***:***" + url[i:]
		}
	}
	return url
}

func buildRegistry(cfg *config.Config) (*provider.Registry, error) {
	overrides := map[provider.Channel]string{}
	for ch, name := range map[provider.Channel]string{
		provider.Email:    cfg.Defaults.Email,
		provider.SMS:      cfg.Defaults.SMS,
		provider.WhatsApp: cfg.Defaults.WhatsApp,
		provider.Push:     cfg.Defaults.Push,
	} {
		if name != "" {
			overrides[ch] = name
		}
	}

	reg := provider.NewRegistry([]provider.Provider{
		provider.NewSMTP(cfg.SMTP),
		provider.NewResend(cfg.Resend),
		provider.NewSES(cfg.SES),
		provider.NewTwilio(cfg.Twilio),
		provider.NewInfobip(cfg.Infobip),
		provider.NewFCM(cfg.FCM),
	}, overrides)

	var errs []string
	for _, ch := range provider.AllChannels {
		name, hasOverride := overrides[ch]
		if !hasOverride {
			continue
		}
		p, ok := reg.ByName(name)
		if !ok {
			errs = append(errs, fmt.Sprintf("DEFAULT_%s_PROVIDER=%q: unknown provider (known: smtp, resend, ses, twilio, infobip, fcm)", strings.ToUpper(string(ch)), name))
			continue
		}
		st, ok := provider.StatusFor(p, ch)
		if !ok || !st.Configured {
			errs = append(errs, fmt.Sprintf("DEFAULT_%s_PROVIDER=%q: provider %q is not configured for channel %q", strings.ToUpper(string(ch)), name, name, ch))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, fmt.Errorf("invalid default provider config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return reg, nil
}

func logProviders(logger *slog.Logger, reg *provider.Registry) {
	for _, p := range reg.Providers() {
		for _, cs := range p.Channels() {
			base := []any{"provider", p.Name(), "channel", string(cs.Channel)}
			if !cs.Configured {
				logger.Info("provider channel not configured", base...)
				continue
			}
			base = append(base, "is_default", reg.IsDefault(p.Name(), cs.Channel))
			for _, k := range sortedKeys(cs.Settings) {
				base = append(base, k, cs.Settings[k])
			}
			logger.Info("provider channel ready", base...)
		}
	}
	for _, ch := range provider.AllChannels {
		if name, ok := reg.Default(ch); ok {
			logger.Info("default provider resolved", "channel", string(ch), "provider", name)
		} else {
			logger.Warn("no default provider", "channel", string(ch))
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
