package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/nexxserve/nexxnotify/internal/db"
	"github.com/nexxserve/nexxnotify/internal/httpapi"
	"github.com/nexxserve/nexxnotify/internal/provider"
)

// testEnv holds the server and database resources for integration tests.
type testEnv struct {
	server *http.Server
	pool   *pgxpool.Pool
	base   string // http://127.0.0.1:<port>
}

func uid() string {
	return fmt.Sprintf("_%d", rand.Intn(999999))
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://nexxnotify:nexxnotify@localhost:5433/nexxnotify?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("connect to test database (skipping): %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("ping test database at %s (skipping): %v", dsn, err)
	}
	t.Cleanup(func() { pool.Close() })

	// Run migrations
	if err := db.MigrateUp(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Seed providers in database
	q := db.New(pool)
	for _, pid := range []string{"twilio", "infobip", "resend", "smtp", "ses", "fcm"} {
		_, _ = q.SeedProvider(ctx, db.SeedProviderParams{ID: pid, DisplayName: pid})
	}

	// Seed providers (empty registry — no providers configured)
	reg := provider.NewRegistry([]provider.Provider{}, nil)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Pick a random free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           httpapi.NewServer(logger, reg, pool, db.New(pool)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
	})

	// Wait for server to be ready
	ready := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	for i := 0; i < 20; i++ {
		resp, err := http.Get(ready)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	return &testEnv{
		server: srv,
		pool:   pool,
		base:   fmt.Sprintf("http://127.0.0.1:%d", port),
	}
}

// --- helpers ---

func doReq(t *testing.T, e *testEnv, method, path string, body any) (int, map[string]any) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, e.base+path, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}

	return resp.StatusCode, result
}

// --- tests ---

func TestIntegration_Healthz(t *testing.T) {
	env := setupTestEnv(t)

	code, body := doReq(t, env, "GET", "/healthz", nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
}

func TestIntegration_FlowCRUD(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "crud_flow" + uid()

	// 1. Create flow
	code, flow := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "CRUD Test Flow",
		"input": map[string]any{
			"allows_variables": true,
			"variables": map[string]any{
				"code": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{
				"channel":       "sms",
				"uses_template": false,
				"default_content": map[string]any{
					"body": "Your code is {{code}}",
				},
				"required_variables": []string{"code"},
			},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d — %v", code, flow)
	}
	if flow["id"] != flowID {
		t.Errorf("flow id = %v, want %s", flow["id"], flowID)
	}
	if flow["active"] != true {
		t.Errorf("flow active = %v, want true", flow["active"])
	}

	// 2. Get flow
	code, got := doReq(t, env, "GET", "/flows/"+flowID, nil)
	if code != 200 {
		t.Fatalf("get flow: expected 200, got %d", code)
	}
	if got["name"] != "CRUD Test Flow" {
		t.Errorf("flow name = %v, want 'CRUD Test Flow'", got["name"])
	}
	channels, ok := got["channels"].([]any)
	if !ok || len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %v", got["channels"])
	}

	// 3. List flows
	code, list := doReq(t, env, "GET", "/flows", nil)
	if code != 200 {
		t.Fatalf("list flows: expected 200, got %d", code)
	}
	flows := list["flows"].([]any)
	if len(flows) < 1 {
		t.Fatalf("expected at least 1 flow, got %d", len(flows))
	}

	// 4. Update flow
	code, updated := doReq(t, env, "PUT", "/flows/"+flowID, map[string]any{
		"name": "CRUD Test Flow v2",
	})
	if code != 200 {
		t.Fatalf("update flow: expected 200, got %d", code)
	}
	if updated["name"] != "CRUD Test Flow v2" {
		t.Errorf("updated name = %v, want 'CRUD Test Flow v2'", updated["name"])
	}

	// 5. Add a second channel
	code, _ = doReq(t, env, "POST", "/flows/"+flowID+"/channels", map[string]any{
		"channel":       "email",
		"uses_template": false,
		"default_content": map[string]any{
			"subject": "Your code",
			"body":    "Code: {{code}}",
		},
		"required_variables": []string{"code"},
	})
	if code != 201 {
		t.Fatalf("add channel: expected 201, got %d", code)
	}

	// 6. Verify 2 channels now
	code, got = doReq(t, env, "GET", "/flows/"+flowID, nil)
	if code != 200 {
		t.Fatalf("get flow after channel add: expected 200, got %d", code)
	}
	channels = got["channels"].([]any)
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}

	// 7. Delete flow
	code, _ = doReq(t, env, "DELETE", "/flows/"+flowID, nil)
	if code != 204 {
		t.Fatalf("delete flow: expected 204, got %d", code)
	}

	// 8. Confirm gone
	code, _ = doReq(t, env, "GET", "/flows/"+flowID, nil)
	if code != 404 {
		t.Errorf("expected 404 after delete, got %d", code)
	}
}

func TestIntegration_Send_VariablesOnly(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "send_vars" + uid()

	// Create flow with variable-based SMS + email
	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "Send Vars Test",
		"input": map[string]any{
			"allows_variables": true,
			"variables": map[string]any{
				"name": map[string]any{"type": "string", "required": true},
				"code": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{
				"channel":       "sms",
				"uses_template": false,
				"default_content": map[string]any{
					"body": "Hi {{name}}, your code is {{code}}",
				},
				"required_variables": []string{"code"},
			},
			{
				"channel":       "email",
				"uses_template": false,
				"default_content": map[string]any{
					"subject": "Your Code",
					"body":    "Hello {{name}}, code: {{code}}",
				},
				"required_variables": []string{"name", "code"},
			},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send with both email and phone → should produce 2 deliveries
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"id":        "ntf_integration_001",
		"variables": map[string]any{"name": "Alice", "code": "123456"},
		"receivers": []map[string]any{
			{"name": "Alice", "email": "alice@test.com", "phone": "+1111111111"},
		},
	})
	if code != 200 {
		t.Fatalf("send: expected 200, got %d — %v", code, result)
	}

	// Results should show 2 deliveries (email + sms)
	results := result["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 delivery results, got %d: %v", len(results), results)
	}

	// Both should be "failed" (no provider configured) — that's fine, the routing is correct
	channelsSeen := map[string]bool{}
	for _, r := range results {
		delivery := r.(map[string]any)
		ch := delivery["channel"].(string)
		channelsSeen[ch] = true
		if delivery["status"] != "failed" {
			t.Errorf("channel %s: expected status 'failed' (no provider), got %v", ch, delivery["status"])
		}
	}
	if !channelsSeen["sms"] {
		t.Error("expected SMS delivery in results")
	}
	if !channelsSeen["email"] {
		t.Error("expected email delivery in results")
	}
}

func TestIntegration_Send_SmsOnlyReceiver(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "sms_only" + uid()

	// Create flow
	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "SMS Only",
		"input": map[string]any{
			"allows_variables": true,
			"variables": map[string]any{
				"msg": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{"channel": "sms", "uses_template": false, "default_content": map[string]any{"body": "{{msg}}"}},
			{"channel": "email", "uses_template": false, "default_content": map[string]any{"subject": "Msg", "body": "{{msg}}"}},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send to phone-only receiver → should only produce SMS delivery
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"variables": map[string]any{"msg": "Hello!"},
		"receivers": []map[string]any{
			{"name": "Bob", "phone": "+2222222222"},
		},
	})
	if code != 200 {
		t.Fatalf("send: expected 200, got %d", code)
	}

	results := result["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 delivery (SMS only), got %d: %v", len(results), results)
	}
	ch := results[0].(map[string]any)["channel"].(string)
	if ch != "sms" {
		t.Errorf("expected channel sms, got %s", ch)
	}
}

func TestIntegration_Send_EmailOnlyReceiver(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "email_only" + uid()

	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "Email Only",
		"input": map[string]any{
			"allows_content": true,
		},
		"channels": []map[string]any{
			{"channel": "sms", "enabled": true},
			{"channel": "email", "enabled": true},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send raw content to email-only receiver
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id": flowID,
		"content": map[string]any{
			"body":    "System maintenance at 2am",
			"subject": "Maintenance Notice",
			"title":   "Alert",
		},
		"receivers": []map[string]any{
			{"name": "Carol", "email": "carol@test.com"},
		},
	})
	if code != 200 {
		t.Fatalf("send: expected 200, got %d", code)
	}

	results := result["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 delivery (email only), got %d: %v", len(results), results)
	}
	ch := results[0].(map[string]any)["channel"].(string)
	if ch != "email" {
		t.Errorf("expected channel email, got %s", ch)
	}
}

func TestIntegration_Send_MultipleReceivers(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "multi_recv" + uid()

	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "Multi Receiver",
		"input": map[string]any{
			"allows_variables": true,
			"variables": map[string]any{
				"alert": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{"channel": "sms", "uses_template": false, "default_content": map[string]any{"body": "{{alert}}"}},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send to 3 receivers
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"variables": map[string]any{"alert": "Server is down!"},
		"receivers": []map[string]any{
			{"name": "User1", "phone": "+111"},
			{"name": "User2", "phone": "+222"},
			{"name": "User3", "phone": "+333", "email": "u3@test.com"},
		},
	})
	if code != 200 {
		t.Fatalf("send: expected 200, got %d", code)
	}

	results := result["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 delivery results, got %d: %v", len(results), results)
	}

	for _, r := range results {
		delivery := r.(map[string]any)
		if delivery["channel"] != "sms" {
			t.Errorf("expected channel sms, got %v", delivery["channel"])
		}
	}
}

func TestIntegration_Send_ChannelContentOverride(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "override" + uid()

	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "Override Test",
		"input": map[string]any{
			"allows_content":   true,
			"allows_variables": true,
			"variables": map[string]any{
				"name": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{
				"channel":         "sms",
				"uses_template":   false,
				"default_content": map[string]any{"body": "Default: Hi {{name}}"},
			},
			{
				"channel":         "email",
				"uses_template":   false,
				"default_content": map[string]any{"subject": "Default Subject", "body": "Default email for {{name}}"},
			},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send with channel-specific content override for email
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"variables": map[string]any{"name": "Dave"},
		"channel_content": map[string]any{
			"email": map[string]any{
				"subject": "Custom Subject",
				"body":    "Custom email for Dave",
			},
		},
		"receivers": []map[string]any{
			{"name": "Dave", "email": "dave@test.com", "phone": "+444"},
		},
	})
	if code != 200 {
		t.Fatalf("send: expected 200, got %d", code)
	}

	results := result["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(results))
	}

	// Both should be "failed" (no provider) but routing worked
	channelsSeen := map[string]bool{}
	for _, r := range results {
		delivery := r.(map[string]any)
		channelsSeen[delivery["channel"].(string)] = true
	}
	if !channelsSeen["sms"] || !channelsSeen["email"] {
		t.Errorf("expected both sms and email, got %v", channelsSeen)
	}
}

func TestIntegration_Send_MissingRequiredVariable(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "validation" + uid()

	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":   flowID,
		"name": "Validation Test",
		"input": map[string]any{
			"allows_variables": true,
			"variables": map[string]any{
				"required_var": map[string]any{"type": "string", "required": true},
			},
		},
		"channels": []map[string]any{
			{"channel": "sms", "uses_template": false, "default_content": map[string]any{"body": "{{required_var}}"}},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Send without the required variable
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"variables": map[string]any{},
		"receivers": []map[string]any{
			{"name": "Test", "phone": "+111"},
		},
	})
	if code != 400 {
		t.Errorf("expected 400 for missing required variable, got %d — %v", code, result)
	}
}

func TestIntegration_Send_InactiveFlow(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "inactive" + uid()

	// Create active flow
	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":     flowID,
		"name":   "Will be deactivated",
		"active": true,
		"input":  map[string]any{},
		"channels": []map[string]any{
			{"channel": "sms", "uses_template": false, "default_content": map[string]any{"body": "hi"}},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	// Deactivate
	code, _ = doReq(t, env, "PUT", "/flows/"+flowID, map[string]any{"active": false})
	if code != 200 {
		t.Fatalf("deactivate: expected 200, got %d", code)
	}

	// Send should fail
	code, result := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"receivers": []map[string]any{{"name": "Test", "phone": "+111"}},
	})
	if code != 400 {
		t.Errorf("expected 400 for inactive flow, got %d — %v", code, result)
	}
}

func TestIntegration_Send_NonexistentFlow(t *testing.T) {
	env := setupTestEnv(t)

	code, _ := doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   "does_not_exist_" + uid(),
		"receivers": []map[string]any{{"name": "Test", "phone": "+111"}},
	})
	if code != 404 {
		t.Errorf("expected 404 for nonexistent flow, got %d", code)
	}
}

func TestIntegration_Send_EmptyReceivers(t *testing.T) {
	env := setupTestEnv(t)
	flowID := "empty_recv" + uid()

	code, _ := doReq(t, env, "POST", "/flows", map[string]any{
		"id":    flowID,
		"name":  "Empty Receivers",
		"input": map[string]any{},
		"channels": []map[string]any{
			{"channel": "sms", "uses_template": false, "default_content": map[string]any{"body": "hi"}},
		},
	})
	if code != 201 {
		t.Fatalf("create flow: expected 201, got %d", code)
	}

	code, _ = doReq(t, env, "POST", "/send", map[string]any{
		"flow_id":   flowID,
		"receivers": []map[string]any{},
	})
	if code != 400 {
		t.Errorf("expected 400 for empty receivers, got %d", code)
	}
}

func randCountryCode() string {
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	return string([]byte{letters[rand.Intn(len(letters))], letters[rand.Intn(len(letters))]})
}

func TestIntegration_Providers(t *testing.T) {
	env := setupTestEnv(t)

	code, body := doReq(t, env, "GET", "/providers", nil)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}
	if _, ok := body["providers"]; !ok {
		t.Fatalf("expected providers key in response, got %v", body)
	}
}

func TestIntegration_CountryCRUD(t *testing.T) {
	env := setupTestEnv(t)
	cc := randCountryCode()

	code, errResp := doReq(t, env, "POST", "/countries", map[string]any{
		"code":         "INVALID",
		"phone_code":   "123",
		"total_digits": 10,
	})
	if code != 400 {
		t.Errorf("expected 400 for invalid country code, got %d: %v", code, errResp)
	}

	code, created := doReq(t, env, "POST", "/countries", map[string]any{
		"code":         cc,
		"phone_code":   "+234",
		"total_digits": 10,
	})
	if code != 201 {
		t.Fatalf("create country: expected 201, got %d — %v", code, created)
	}
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	})
	if created["code"] != cc || created["phone_code"] != "234" {
		t.Errorf("unexpected country data: %v", created)
	}

	code, _ = doReq(t, env, "POST", "/countries", map[string]any{
		"code":         cc,
		"phone_code":   "234",
		"total_digits": 10,
	})
	if code != 409 {
		t.Errorf("expected 409 for duplicate country, got %d", code)
	}

	code, got := doReq(t, env, "GET", "/countries/"+cc, nil)
	if code != 200 {
		t.Fatalf("get country: expected 200, got %d", code)
	}
	if got["code"] != cc {
		t.Errorf("expected %s, got %v", cc, got["code"])
	}

	code, _ = doReq(t, env, "GET", "/countries/ZZ", nil)
	if code != 404 {
		t.Errorf("expected 404 for unknown country, got %d", code)
	}

	code, list := doReq(t, env, "GET", "/countries", nil)
	if code != 200 {
		t.Fatalf("list countries: expected 200, got %d", code)
	}
	countries := list["countries"].([]any)
	if len(countries) == 0 {
		t.Errorf("expected non-empty countries list")
	}

	code, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	if code != 204 {
		t.Fatalf("delete country: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/countries/ZZ", nil)
	if code != 404 {
		t.Errorf("expected 404 for deleting nonexistent country, got %d", code)
	}
}

func TestIntegration_CarrierCRUD(t *testing.T) {
	env := setupTestEnv(t)
	cc := randCountryCode()

	code, _ := doReq(t, env, "POST", "/countries", map[string]any{
		"code":         cc,
		"phone_code":   "254",
		"total_digits": 9,
	})
	if code != 201 {
		t.Fatalf("setup country %s failed: %d", cc, code)
	}
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	})

	code, carrier := doReq(t, env, "POST", "/carriers", map[string]any{
		"country_code": cc,
		"name":         "Safaricom",
		"prefixes":     []string{"701", "702"},
	})
	if code != 201 {
		t.Fatalf("create carrier: expected 201, got %d — %v", code, carrier)
	}
	carrierID := carrier["id"].(string)
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/carriers/"+carrierID, nil)
	})

	code, _ = doReq(t, env, "POST", "/carriers", map[string]any{
		"country_code": cc,
		"name":         "Safaricom",
		"prefixes":     []string{"703"},
	})
	if code != 409 {
		t.Errorf("expected 409 for duplicate carrier, got %d", code)
	}

	code, _ = doReq(t, env, "GET", "/carriers", nil)
	if code != 400 {
		t.Errorf("expected 400 when country param is missing, got %d", code)
	}

	code, list := doReq(t, env, "GET", "/carriers?country="+cc, nil)
	if code != 200 {
		t.Fatalf("list carriers: expected 200, got %d", code)
	}
	carriers := list["carriers"].([]any)
	if len(carriers) != 1 {
		t.Errorf("expected 1 carrier, got %d", len(carriers))
	}

	code, got := doReq(t, env, "GET", "/carriers/"+carrierID, nil)
	if code != 200 {
		t.Fatalf("get carrier: expected 200, got %d", code)
	}
	if got["name"] != "Safaricom" {
		t.Errorf("expected carrier name Safaricom, got %v", got["name"])
	}

	code, _ = doReq(t, env, "DELETE", "/carriers/"+carrierID, nil)
	if code != 204 {
		t.Fatalf("delete carrier: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	if code != 204 {
		t.Fatalf("delete country: expected 204, got %d", code)
	}
}

func TestIntegration_PricingCRUD(t *testing.T) {
	env := setupTestEnv(t)
	cc := randCountryCode()

	code, _ := doReq(t, env, "POST", "/countries", map[string]any{
		"code":         cc,
		"phone_code":   "233",
		"total_digits": 9,
	})
	if code != 201 {
		t.Fatalf("setup country %s failed: %d", cc, code)
	}
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	})

	code, carrier := doReq(t, env, "POST", "/carriers", map[string]any{
		"country_code": cc,
		"name":         "MTN",
		"prefixes":     []string{"24", "54"},
	})
	if code != 201 {
		t.Fatalf("setup carrier MTN failed: %d", code)
	}
	carrierID := carrier["id"].(string)
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/carriers/"+carrierID, nil)
	})

	code, pricing := doReq(t, env, "POST", "/pricing", map[string]any{
		"provider":     "twilio",
		"channel":      "sms",
		"country_code": cc,
		"carrier_id":   carrierID,
		"tiers": []map[string]any{
			{"min_volume": 0, "max_volume": 999, "tier_price": "0.05"},
			{"min_volume": 1000, "max_volume": 4999, "tier_price": "0.04"},
		},
	})
	if code != 201 {
		t.Fatalf("create pricing: expected 201, got %d — %v", code, pricing)
	}
	pricingID := pricing["id"].(string)
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/pricing/"+pricingID, nil)
	})

	tiers := pricing["tiers"].([]any)
	if len(tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(tiers))
	}

	code, emailPricing := doReq(t, env, "POST", "/pricing", map[string]any{
		"provider": "resend",
		"channel":  "email",
		"tiers": []map[string]any{
			{"min_volume": 0, "max_volume": 10000, "tier_price": "0.001"},
		},
	})
	if code != 201 {
		t.Fatalf("create email pricing: expected 201, got %d — %v", code, emailPricing)
	}
	emailPricingID := emailPricing["id"].(string)
	t.Cleanup(func() {
		_, _ = doReq(t, env, "DELETE", "/pricing/"+emailPricingID, nil)
	})

	code, _ = doReq(t, env, "POST", "/pricing", map[string]any{
		"provider":     "resend",
		"channel":      "email",
		"country_code": cc,
	})
	if code != 400 {
		t.Errorf("expected 400 for email pricing with country_code, got %d", code)
	}

	code, got := doReq(t, env, "GET", "/pricing/"+pricingID, nil)
	if code != 200 {
		t.Fatalf("get pricing: expected 200, got %d", code)
	}
	if got["provider"] != "twilio" || got["channel"] != "sms" {
		t.Errorf("unexpected pricing details: %v", got)
	}

	code, list := doReq(t, env, "GET", "/pricing?provider=twilio&channel=sms&country="+cc, nil)
	if code != 200 {
		t.Fatalf("list pricing: expected 200, got %d", code)
	}
	pricingList := list["pricing"].([]any)
	if len(pricingList) != 1 {
		t.Errorf("expected 1 pricing result, got %d", len(pricingList))
	}

	code, newTier := doReq(t, env, "POST", "/pricing/"+pricingID+"/tiers", map[string]any{
		"min_volume": 5000,
		"max_volume": 10000,
		"tier_price": "0.03",
	})
	if code != 201 {
		t.Fatalf("add tier: expected 201, got %d — %v", code, newTier)
	}
	tierID := newTier["id"].(string)

	code, _ = doReq(t, env, "POST", "/pricing/"+pricingID+"/tiers", map[string]any{
		"min_volume": 2000,
		"max_volume": 3000,
		"tier_price": "0.035",
	})
	if code != 409 {
		t.Errorf("expected 409 for overlapping tier, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/pricing/"+pricingID+"/tiers/"+tierID, nil)
	if code != 204 {
		t.Fatalf("delete tier: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/pricing/"+pricingID, nil)
	if code != 204 {
		t.Fatalf("delete pricing: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/pricing/"+emailPricingID, nil)
	if code != 204 {
		t.Fatalf("delete email pricing: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/carriers/"+carrierID, nil)
	if code != 204 {
		t.Fatalf("delete carrier: expected 204, got %d", code)
	}

	code, _ = doReq(t, env, "DELETE", "/countries/"+cc, nil)
	if code != 204 {
		t.Fatalf("delete country: expected 204, got %d", code)
	}
}
