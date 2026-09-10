package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// EmailMessage represents an email to send.
type EmailMessage struct {
	From     string
	To       string
	Subject  string
	HTMLBody string
	TextBody string
}

// SMSMessage represents an SMS to send.
type SMSMessage struct {
	From string
	To   string
	Body string
}

// PushMessage represents a push notification to send via FCM.
type PushMessage struct {
	Token string // FCM registration token
	Title string
	Body  string
	Data  map[string]string
}

// SendResult is the outcome of a send operation.
type SendResult struct {
	OK      bool
	Message string // provider message ID or error description
}

// Sender dispatches messages through a provider. Each provider
// that supports sending implements this interface for its channels.
type Sender interface {
	// SendEmail sends an email via this provider.
	SendEmail(ctx context.Context, msg EmailMessage) (SendResult, error)
}

// SMSSender is implemented by providers that support SMS.
type SMSSender interface {
	SendSMS(ctx context.Context, msg SMSMessage) (SendResult, error)
}

// PushSender is implemented by providers that support push notifications.
type PushSender interface {
	SendPush(ctx context.Context, msg PushMessage) (SendResult, error)
}

// WhatsAppSender is implemented by providers that support WhatsApp.
type WhatsAppSender interface {
	SendWhatsApp(ctx context.Context, msg SMSMessage) (SendResult, error)
}

// maxSendAttempts is how many times a single provider is tried for a transient
// failure before the registry fails over to the next configured provider.
const maxSendAttempts = 3

// sendBackoff returns the delay before retry attempt n (1-based): 100ms, 200ms,
// capped at 500ms.
func sendBackoff(attempt int) time.Duration {
	d := 100 * time.Millisecond << (attempt - 1)
	if d > 500*time.Millisecond {
		return 500 * time.Millisecond
	}
	return d
}

// shouldRetry reports whether a provider error looks transient (network
// failures, timeouts, provider 5xx/429) and is worth retrying. Permanent
// errors — bad credentials, invalid addresses, missing configuration — return
// false so the send fails (or fails over) immediately.
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"timeout", "i/o timeout", "connection refused", "connection reset",
		"broken pipe", "no such host", "eof", "temporary", "unavailable",
		"too many requests", "api error 429", "api error 5", "token exchange error 5",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// retryTransient calls fn, retrying with a short backoff while the error looks
// transient, and returns the first permanent error (or the last transient error
// after maxSendAttempts) untouched. The caller's context is respected between
// attempts.
func retryTransient(ctx context.Context, fn func() (SendResult, error)) (SendResult, error) {
	var res SendResult
	var err error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		res, err = fn()
		if err == nil || !shouldRetry(err) {
			return res, err
		}
		if attempt < maxSendAttempts {
			if err := sleepCtx(ctx, sendBackoff(attempt)); err != nil {
				return res, err
			}
		}
	}
	return res, err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// configuredProviders lists the providers that can serve a channel, with the
// configured default first (when set) followed by every other configured
// provider in registration order. This ordering makes failover deterministic:
// the preferred provider is tried first, then the rest.
func (r *Registry) configuredProviders(ch Channel) []Provider {
	override, hasOverride := r.defaults[ch]
	var ordered []Provider
	if hasOverride {
		if p, ok := r.ByName(override); ok {
			if st, ok := StatusFor(p, ch); ok && st.Configured {
				ordered = append(ordered, p)
			}
		}
	}
	for _, p := range r.providers {
		if hasOverride && p.Name() == override {
			continue
		}
		if st, ok := StatusFor(p, ch); ok && st.Configured {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

// dispatchWithFailover sends through the configured providers for a channel:
// transient failures are retried per provider and, once a provider is
// exhausted, the send fails over to the next configured provider. The error
// from the last attempt is returned when every provider failed.
func (r *Registry) dispatchWithFailover(ctx context.Context, channel Channel,
	send func(p Provider) (SendResult, error)) (SendResult, error) {

	providers := r.configuredProviders(channel)
	if len(providers) == 0 {
		return SendResult{}, fmt.Errorf("no configured provider for channel %s", channel)
	}

	var lastErr error
	for _, p := range providers {
		res, err := retryTransient(ctx, func() (SendResult, error) { return send(p) })
		if err == nil {
			return res, nil
		}
		lastErr = fmt.Errorf("%s: %w", p.Name(), err)
	}
	return SendResult{}, fmt.Errorf("all providers failed for channel %s: %w", channel, lastErr)
}

// SendEmail dispatches an email message through the appropriate provider
// in the registry, resolved by channel.
func (r *Registry) SendEmail(ctx context.Context, channel Channel, msg EmailMessage) (SendResult, error) {
	return r.dispatchWithFailover(ctx, channel, func(p Provider) (SendResult, error) {
		sender, ok := p.(Sender)
		if !ok {
			return SendResult{}, fmt.Errorf("provider %s cannot send email", p.Name())
		}
		return sender.SendEmail(ctx, msg)
	})
}

// SendSMS dispatches an SMS message through the appropriate provider.
func (r *Registry) SendSMS(ctx context.Context, channel Channel, msg SMSMessage) (SendResult, error) {
	return r.dispatchWithFailover(ctx, channel, func(p Provider) (SendResult, error) {
		sender, ok := p.(SMSSender)
		if !ok {
			return SendResult{}, fmt.Errorf("provider %s cannot send sms", p.Name())
		}
		return sender.SendSMS(ctx, msg)
	})
}

// SendPush dispatches a push notification through the appropriate provider.
func (r *Registry) SendPush(ctx context.Context, channel Channel, msg PushMessage) (SendResult, error) {
	return r.dispatchWithFailover(ctx, channel, func(p Provider) (SendResult, error) {
		sender, ok := p.(PushSender)
		if !ok {
			return SendResult{}, fmt.Errorf("provider %s cannot send push", p.Name())
		}
		return sender.SendPush(ctx, msg)
	})
}

// SendWhatsApp dispatches a WhatsApp message through the appropriate provider.
func (r *Registry) SendWhatsApp(ctx context.Context, channel Channel, msg SMSMessage) (SendResult, error) {
	return r.dispatchWithFailover(ctx, channel, func(p Provider) (SendResult, error) {
		sender, ok := p.(WhatsAppSender)
		if !ok {
			return SendResult{}, fmt.Errorf("provider %s cannot send whatsapp", p.Name())
		}
		return sender.SendWhatsApp(ctx, msg)
	})
}
