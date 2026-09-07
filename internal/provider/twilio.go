package provider

import "github.com/nexxserve/nexxnotify/internal/config"

type Twilio struct {
	cfg config.TwilioConfig
}

func NewTwilio(cfg config.TwilioConfig) *Twilio {
	return &Twilio{cfg: cfg}
}

func (p *Twilio) Name() string { return "twilio" }

func (p *Twilio) Channels() []ChannelStatus {
	baseConfigured := p.cfg.AccountSID != "" && p.cfg.AuthToken != ""

	baseSettings := func(extra map[string]string) map[string]string {
		s := map[string]string{
			"account_sid": Mask(p.cfg.AccountSID),
			"auth_token":  Mask(p.cfg.AuthToken),
		}
		for k, v := range extra {
			s[k] = v
		}
		return s
	}

	smsSettings := map[string]string{}
	if baseConfigured && p.cfg.SMSFrom != "" {
		smsSettings = baseSettings(map[string]string{"from": p.cfg.SMSFrom})
	}

	whatsappSettings := map[string]string{}
	if baseConfigured && p.cfg.WhatsAppFrom != "" {
		whatsappSettings = baseSettings(map[string]string{"from": p.cfg.WhatsAppFrom})
	}

	emailSettings := map[string]string{}
	emailConfigured := baseConfigured && p.cfg.SendGridAPIKey != ""
	if emailConfigured {
		emailSettings = map[string]string{
			"account_sid":   Mask(p.cfg.AccountSID),
			"sendgrid_key":  Mask(p.cfg.SendGridAPIKey),
			"sendgrid_from": p.cfg.SendGridFrom,
		}
	}

	return []ChannelStatus{
		{Channel: SMS, Configured: baseConfigured && p.cfg.SMSFrom != "", Settings: smsSettings},
		{Channel: WhatsApp, Configured: baseConfigured && p.cfg.WhatsAppFrom != "", Settings: whatsappSettings},
		{Channel: Email, Configured: emailConfigured, Settings: emailSettings},
	}
}
