package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// resendSendRequest is the Resend API request body.
type resendSendRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html,omitempty"`
	Text    string `json:"text,omitempty"`
}

// resendSendResponse is the Resend API response.
type resendSendResponse struct {
	ID string `json:"id"`
}

// SendEmail sends an email via the Resend API.
func (p *Resend) SendEmail(ctx context.Context, msg EmailMessage) (SendResult, error) {
	if p.cfg.APIKey == "" {
		return SendResult{}, fmt.Errorf("resend: API key not configured")
	}

	from := p.cfg.From
	if from == "" {
		from = msg.From
	}

	reqBody := resendSendRequest{
		From:    from,
		To:      msg.To,
		Subject: msg.Subject,
		HTML:    msg.HTMLBody,
		Text:    msg.TextBody,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return SendResult{}, fmt.Errorf("resend: API error %d: %s", resp.StatusCode, string(respBody))
	}

	var sendResp resendSendResponse
	if err := json.Unmarshal(respBody, &sendResp); err != nil {
		return SendResult{}, fmt.Errorf("resend: decode response: %w", err)
	}

	return SendResult{OK: true, Message: sendResp.ID}, nil
}
