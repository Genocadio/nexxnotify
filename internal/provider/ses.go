package provider

import "github.com/nexxserve/nexxnotify/internal/config"

type SES struct {
	cfg config.SESConfig
}

func NewSES(cfg config.SESConfig) *SES {
	return &SES{cfg: cfg}
}

func (p *SES) Name() string { return "ses" }

func (p *SES) Channels() []ChannelStatus {
	settings := map[string]string{}
	configured := p.cfg.AccessKeyID != "" && p.cfg.Region != ""
	if configured {
		settings = map[string]string{
			"region":     p.cfg.Region,
			"access_key": Mask(p.cfg.AccessKeyID),
			"secret_key": Mask(p.cfg.SecretAccessKey),
			"from":       p.cfg.From,
		}
	}
	return []ChannelStatus{
		{Channel: Email, Configured: configured, Settings: settings},
	}
}
