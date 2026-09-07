package provider

type Channel string

const (
	Email    Channel = "email"
	SMS      Channel = "sms"
	WhatsApp Channel = "whatsapp"
	Push     Channel = "push"
)

var AllChannels = []Channel{Email, SMS, WhatsApp, Push}

type ChannelStatus struct {
	Channel    Channel
	Configured bool
	Settings   map[string]string
}

type Provider interface {
	Name() string
	Channels() []ChannelStatus
}

func Mask(secret string) string {
	switch {
	case secret == "":
		return ""
	case len(secret) <= 2:
		return "****"
	default:
		return "****" + secret[len(secret)-2:]
	}
}

func StatusFor(p Provider, ch Channel) (ChannelStatus, bool) {
	for _, cs := range p.Channels() {
		if cs.Channel == ch {
			return cs, true
		}
	}
	return ChannelStatus{}, false
}
