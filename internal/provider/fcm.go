package provider

import "github.com/nexxserve/nexxnotify/internal/config"

type FCM struct {
	cfg config.FCMConfig
}

func NewFCM(cfg config.FCMConfig) *FCM {
	return &FCM{cfg: cfg}
}

func (p *FCM) Name() string { return "fcm" }

func (p *FCM) Channels() []ChannelStatus {
	settings := map[string]string{}
	if p.cfg.Credentials != "" {
		settings = map[string]string{
			"project_id":   p.cfg.ProjectID,
			"client_email": p.cfg.ClientEmail,
			"credentials":  Mask(p.cfg.Credentials),
		}
	}
	return []ChannelStatus{
		{Channel: Push, Configured: p.cfg.Credentials != "", Settings: settings},
	}
}
