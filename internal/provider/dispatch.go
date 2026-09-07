package provider

import (
	"context"
	"fmt"
)

// DispatchEmail sends an email via the appropriate provider for the "email" channel.
func (r *Registry) DispatchEmail(ctx context.Context, to, subject, htmlBody, textBody string) (SendResult, error) {
	return r.SendEmail(ctx, Email, EmailMessage{
		To:       to,
		Subject:  subject,
		HTMLBody: htmlBody,
		TextBody: textBody,
	})
}

// DispatchSMS sends an SMS via the appropriate provider for the "sms" channel.
func (r *Registry) DispatchSMS(ctx context.Context, to, body string) (SendResult, error) {
	return r.SendSMS(ctx, SMS, SMSMessage{
		To:   to,
		Body: body,
	})
}

// DispatchWhatsApp sends a WhatsApp message via the appropriate provider for the "whatsapp" channel.
func (r *Registry) DispatchWhatsApp(ctx context.Context, to, body string) (SendResult, error) {
	return r.SendWhatsApp(ctx, WhatsApp, SMSMessage{
		To:   to,
		Body: body,
	})
}

// DispatchPush sends a push notification via the appropriate provider for the "push" channel.
func (r *Registry) DispatchPush(ctx context.Context, token, title, body string, data map[string]string) (SendResult, error) {
	return r.SendPush(ctx, Push, PushMessage{
		Token: token,
		Title: title,
		Body:  body,
		Data:  data,
	})
}

// Dispatch sends a resolved channel payload through the appropriate provider.
// This is the main entry point for the flow dispatch layer.
func (r *Registry) Dispatch(ctx context.Context, channel, to string, subject, title, body, html string, data map[string]string) (SendResult, error) {
	switch channel {
	case "email":
		return r.DispatchEmail(ctx, to, subject, html, body)
	case "sms":
		return r.DispatchSMS(ctx, to, body)
	case "whatsapp":
		return r.DispatchWhatsApp(ctx, to, body)
	case "fcm", "push":
		return r.DispatchPush(ctx, to, title, body, data)
	default:
		return SendResult{}, fmt.Errorf("unsupported channel: %s", channel)
	}
}
