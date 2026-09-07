package provider

import "github.com/nexxserve/nexxnotify/internal/config"

type Resend struct {
	cfg config.ResendConfig
}

func NewResend(cfg config.ResendConfig) *Resend {
	return &Resend{cfg: cfg}
}

func (p *Resend) Name() string { return "resend" }

func (p *Resend) Channels() []ChannelStatus {
	settings := map[string]string{}
	if p.cfg.APIKey != "" {
		settings = map[string]string{
			"api_key": Mask(p.cfg.APIKey),
			"from":    p.cfg.From,
		}
	}
	return []ChannelStatus{
		{Channel: Email, Configured: p.cfg.APIKey != "", Settings: settings},
	}
}
