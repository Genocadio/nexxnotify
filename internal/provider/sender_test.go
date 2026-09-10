package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeEmailProvider is a configurable email provider for registry tests.
type fakeEmailProvider struct {
	name      string
	failures  int  // remaining consecutive failures before a successful send
	transient bool // whether failures are transient (retryable)
	calls     int
}

func (p *fakeEmailProvider) Name() string { return p.name }

func (p *fakeEmailProvider) Channels() []ChannelStatus {
	return []ChannelStatus{{Channel: Email, Configured: true, Settings: map[string]string{}}}
}

func (p *fakeEmailProvider) SendEmail(_ context.Context, _ EmailMessage) (SendResult, error) {
	p.calls++
	if p.failures > 0 {
		p.failures--
		if p.transient {
			return SendResult{}, errors.New(p.name + ": API error 503: upstream busy")
		}
		return SendResult{}, errors.New(p.name + ": API error 401: bad credentials")
	}
	return SendResult{OK: true, Message: "msg-" + p.name}, nil
}

func newTestRegistry(providers []Provider, overrides map[Channel]string) *Registry {
	return NewRegistry(providers, overrides)
}

func TestShouldRetryClassification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("resend: API error 500: upstream exploded"), true},
		{errors.New("twilio: API error 429: slow down"), true},
		{errors.New("smtp: connect: dial tcp 1.2.3.4: i/o timeout"), true},
		{errors.New("resend: connection refused"), true},
		{errors.New("fcm: token exchange error 502: bad gateway"), true},
		{errors.New("twilio: API error 401: invalid credentials"), false},
		{errors.New("resend: API key not configured"), false},
	}
	for _, c := range cases {
		if got := shouldRetry(c.err); got != c.want {
			t.Errorf("shouldRetry(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestSendRetriesTransientFailures(t *testing.T) {
	p := &fakeEmailProvider{name: "resend", failures: 2, transient: true}
	reg := newTestRegistry([]Provider{p}, nil)

	res, err := reg.SendEmail(context.Background(), Email, EmailMessage{To: "a@b.com"})
	if err != nil {
		t.Fatalf("SendEmail: unexpected error: %v", err)
	}
	if !res.OK || res.Message != "msg-resend" {
		t.Errorf("res = %+v, want successful send from resend", res)
	}
	if p.calls != 3 {
		t.Errorf("calls = %d, want 3 (2 transient failures + 1 success)", p.calls)
	}
}

func TestSendFailsOverToNextProvider(t *testing.T) {
	a := &fakeEmailProvider{name: "resend", failures: 1, transient: false}
	b := &fakeEmailProvider{name: "smtp"}
	reg := newTestRegistry([]Provider{a, b}, nil)

	res, err := reg.SendEmail(context.Background(), Email, EmailMessage{To: "a@b.com"})
	if err != nil {
		t.Fatalf("SendEmail: unexpected error: %v", err)
	}
	if res.Message != "msg-smtp" {
		t.Errorf("res.Message = %q, want %q (failover to smtp)", res.Message, "msg-smtp")
	}
	if a.calls != 1 {
		t.Errorf("resend calls = %d, want 1 (permanent failure, no retry)", a.calls)
	}
	if b.calls != 1 {
		t.Errorf("smtp calls = %d, want 1", b.calls)
	}
}

func TestSendPrefersDefaultProviderThenFailsOver(t *testing.T) {
	a := &fakeEmailProvider{name: "resend"}
	b := &fakeEmailProvider{name: "smtp", failures: 1, transient: false}
	reg := newTestRegistry([]Provider{a, b}, map[Channel]string{Email: "smtp"})

	res, err := reg.SendEmail(context.Background(), Email, EmailMessage{To: "a@b.com"})
	if err != nil {
		t.Fatalf("SendEmail: unexpected error: %v", err)
	}
	if res.Message != "msg-resend" {
		t.Errorf("res.Message = %q, want %q (default smtp tried first, then resend)", res.Message, "msg-resend")
	}
	if b.calls != 1 || a.calls != 1 {
		t.Errorf("calls = smtp:%d resend:%d, want 1 and 1", b.calls, a.calls)
	}
}

func TestSendReturnsErrorWhenAllProvidersFail(t *testing.T) {
	a := &fakeEmailProvider{name: "resend", failures: 1, transient: false}
	b := &fakeEmailProvider{name: "smtp", failures: 1, transient: false}
	reg := newTestRegistry([]Provider{a, b}, nil)

	_, err := reg.SendEmail(context.Background(), Email, EmailMessage{To: "a@b.com"})
	if err == nil {
		t.Fatal("SendEmail: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "all providers failed") {
		t.Errorf("error = %q, want to mention all providers failed", err.Error())
	}
}

func TestSendFailsImmediatelyWhenNoProviderConfigured(t *testing.T) {
	reg := newTestRegistry([]Provider{}, nil)

	_, err := reg.SendEmail(context.Background(), Email, EmailMessage{To: "a@b.com"})
	if err == nil {
		t.Fatal("SendEmail: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no configured provider") {
		t.Errorf("error = %q, want to mention no configured provider", err.Error())
	}
}
