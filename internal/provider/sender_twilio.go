package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SendSMS sends an SMS via the Twilio REST API.
func (p *Twilio) SendSMS(ctx context.Context, msg SMSMessage) (SendResult, error) {
	if p.cfg.AccountSID == "" || p.cfg.AuthToken == "" {
		return SendResult{}, fmt.Errorf("twilio: credentials not configured")
	}

	from := p.cfg.SMSFrom
	if from == "" {
		from = msg.From
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.cfg.AccountSID)

	data := url.Values{}
	data.Set("From", from)
	data.Set("To", msg.To)
	data.Set("Body", msg.Body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return SendResult{}, fmt.Errorf("twilio: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(p.cfg.AccountSID, p.cfg.AuthToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("twilio: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return SendResult{}, fmt.Errorf("twilio: API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Twilio returns a SID in the response
	return SendResult{OK: true, Message: string(respBody)}, nil
}

// SendWhatsApp sends a WhatsApp message via the Twilio API.
func (p *Twilio) SendWhatsApp(ctx context.Context, msg SMSMessage) (SendResult, error) {
	if p.cfg.AccountSID == "" || p.cfg.AuthToken == "" {
		return SendResult{}, fmt.Errorf("twilio: credentials not configured")
	}

	from := p.cfg.WhatsAppFrom
	if from == "" {
		from = msg.From
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.cfg.AccountSID)

	data := url.Values{}
	data.Set("From", from)
	data.Set("To", msg.To)
	data.Set("Body", msg.Body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return SendResult{}, fmt.Errorf("twilio: create whatsapp request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.SetBasicAuth(p.cfg.AccountSID, p.cfg.AuthToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("twilio: send whatsapp request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return SendResult{}, fmt.Errorf("twilio: whatsapp API error %d: %s", resp.StatusCode, string(respBody))
	}

	return SendResult{OK: true, Message: string(respBody)}, nil
}
