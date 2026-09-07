package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fcmV1Request is the Firebase HTTP v1 API request body.
type fcmV1Request struct {
	Message fcmV1Message `json:"message"`
}

type fcmV1Message struct {
	Token string            `json:"token"`
	Notification *fcmNotification `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// fcmV1Response is the Firebase HTTP v1 API response body.
type fcmV1Response struct {
	Name string `json:"name"` // message ID
}

// fcmTokenResponse is used to get an OAuth2 access token from the service account.
type fcmTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// SendPush sends a push notification via Firebase Cloud Messaging HTTP v1 API.
func (p *FCM) SendPush(ctx context.Context, msg PushMessage) (SendResult, error) {
	if p.cfg.Credentials == "" {
		return SendResult{}, fmt.Errorf("fcm: credentials not configured")
	}

	// Get OAuth2 access token from service account
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return SendResult{}, fmt.Errorf("fcm: get access token: %w", err)
	}

	fcmReq := fcmV1Request{
		Message: fcmV1Message{
			Token: msg.Token,
			Notification: &fcmNotification{
				Title: msg.Title,
				Body:  msg.Body,
			},
			Data: msg.Data,
		},
	}

	body, err := json.Marshal(fcmReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("fcm: marshal request: %w", err)
	}

	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", p.cfg.ProjectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return SendResult{}, fmt.Errorf("fcm: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return SendResult{}, fmt.Errorf("fcm: send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return SendResult{}, fmt.Errorf("fcm: API error %d: %s", resp.StatusCode, string(respBody))
	}

	var sendResp fcmV1Response
	if err := json.Unmarshal(respBody, &sendResp); err != nil {
		return SendResult{}, fmt.Errorf("fcm: decode response: %w", err)
	}

	return SendResult{OK: true, Message: sendResp.Name}, nil
}

// getAccessToken exchanges the service account's private key for an OAuth2
// access token using Google's token endpoint.
func (p *FCM) getAccessToken(ctx context.Context) (string, error) {
	// For simplicity, we use the Google OAuth2 token endpoint with a JWT.
	// In production, use the official Firebase Admin SDK.
	// This is a minimal implementation using the service account's credentials.

	// Parse the service account JSON to get the private key and client email
	var sa struct {
		PrivateKey  string `json:"private_key"`
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal([]byte(p.cfg.Credentials), &sa); err != nil {
		return "", fmt.Errorf("parse service account: %w", err)
	}

	// Create a JWT for OAuth2 token exchange
	jwt, err := createJWT(sa.ClientEmail, sa.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("create JWT: %w", err)
	}

	// Exchange JWT for access token
	data := fmt.Sprintf("grant_type=urn%%3Aietf%%3Aparams%%3Aoauth%%3Agrant-type%%3Ajwt-bearer&assertion=%s", jwt)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("token exchange error %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp fcmTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	return tokenResp.AccessToken, nil
}
