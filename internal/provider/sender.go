package provider

import (
	"context"
	"fmt"
)

// EmailMessage represents an email to send.
type EmailMessage struct {
	From        string
	To          string
	Subject     string
	HTMLBody    string
	TextBody    string
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

// SendEmail dispatches an email message through the appropriate provider
// in the registry, resolved by channel.
func (r *Registry) SendEmail(ctx context.Context, channel Channel, msg EmailMessage) (SendResult, error) {
	for _, p := range r.providers {
		sender, ok := p.(Sender)
		if !ok {
			continue
		}
		st, ok := StatusFor(p, channel)
		if !ok || !st.Configured {
			continue
		}
		// Use override if set, otherwise first configured provider
		name := p.Name()
		if override, has := r.defaults[channel]; has && override != name {
			if override != name {
				continue
			}
		}
		return sender.SendEmail(ctx, msg)
	}
	return SendResult{}, fmt.Errorf("no configured provider for channel %s", channel)
}

// SendSMS dispatches an SMS message through the appropriate provider.
func (r *Registry) SendSMS(ctx context.Context, channel Channel, msg SMSMessage) (SendResult, error) {
	for _, p := range r.providers {
		sender, ok := p.(SMSSender)
		if !ok {
			continue
		}
		st, ok := StatusFor(p, channel)
		if !ok || !st.Configured {
			continue
		}
		name := p.Name()
		if override, has := r.defaults[channel]; has && override != name {
			if override != name {
				continue
			}
		}
		return sender.SendSMS(ctx, msg)
	}
	return SendResult{}, fmt.Errorf("no configured provider for channel %s", channel)
}

// SendPush dispatches a push notification through the appropriate provider.
func (r *Registry) SendPush(ctx context.Context, channel Channel, msg PushMessage) (SendResult, error) {
	for _, p := range r.providers {
		sender, ok := p.(PushSender)
		if !ok {
			continue
		}
		st, ok := StatusFor(p, channel)
		if !ok || !st.Configured {
			continue
		}
		name := p.Name()
		if override, has := r.defaults[channel]; has && override != name {
			if override != name {
				continue
			}
		}
		return sender.SendPush(ctx, msg)
	}
	return SendResult{}, fmt.Errorf("no configured provider for channel %s", channel)
}

// SendWhatsApp dispatches a WhatsApp message through the appropriate provider.
func (r *Registry) SendWhatsApp(ctx context.Context, channel Channel, msg SMSMessage) (SendResult, error) {
	for _, p := range r.providers {
		sender, ok := p.(WhatsAppSender)
		if !ok {
			continue
		}
		st, ok := StatusFor(p, channel)
		if !ok || !st.Configured {
			continue
		}
		name := p.Name()
		if override, has := r.defaults[channel]; has && override != name {
			if override != name {
				continue
			}
		}
		return sender.SendWhatsApp(ctx, msg)
	}
	return SendResult{}, fmt.Errorf("no configured provider for channel %s", channel)
}
