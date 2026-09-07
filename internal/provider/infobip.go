package provider

import "github.com/nexxserve/nexxnotify/internal/config"

type Infobip struct {
	cfg config.InfobipConfig
}

func NewInfobip(cfg config.InfobipConfig) *Infobip {
	return &Infobip{cfg: cfg}
}

func (p *Infobip) Name() string { return "infobip" }

func (p *Infobip) Channels() []ChannelStatus {
	baseConfigured := p.cfg.APIKey != "" && p.cfg.BaseURL != ""

	smsSettings := map[string]string{}
	if baseConfigured && p.cfg.SMSFrom != "" {
		smsSettings = map[string]string{
			"base_url": p.cfg.BaseURL,
			"api_key":  Mask(p.cfg.APIKey),
			"from":     p.cfg.SMSFrom,
		}
	}

	whatsappSettings := map[string]string{}
	if baseConfigured && p.cfg.WhatsAppFrom != "" {
		whatsappSettings = map[string]string{
			"base_url": p.cfg.BaseURL,
			"api_key":  Mask(p.cfg.APIKey),
			"from":     p.cfg.WhatsAppFrom,
		}
	}

	return []ChannelStatus{
		{Channel: SMS, Configured: baseConfigured && p.cfg.SMSFrom != "", Settings: smsSettings},
		{Channel: WhatsApp, Configured: baseConfigured && p.cfg.WhatsAppFrom != "", Settings: whatsappSettings},
	}
}
