package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nexxserve/nexxnotify/internal/flow"
)

type sendReq struct {
	FlowID           string                          `json:"flow_id"`
	ID               string                          `json:"id"`
	Variables        map[string]any                   `json:"variables,omitempty"`
	Content          *flow.MessageContent             `json:"content,omitempty"`
	ChannelContent   map[string]*flow.MessageContent  `json:"channel_content,omitempty"`
	ChannelVariables map[string]map[string]any        `json:"channel_variables,omitempty"`
	Receivers        []flow.Receiver                  `json:"receivers"`
}

type sendResponse struct {
	ID      string                 `json:"id"`
	Status  string                 `json:"status"`
	Results []sendDeliveryResult   `json:"results"`
}

type sendDeliveryResult struct {
	Receiver string `json:"receiver"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// handleSend triggers a flow with receivers and dispatches to providers.
//
// POST /send
//
//	{
//	  "flow_id": "flow_order_shipped",
//	  "id": "ntf_abc123",
//	  "variables": {
//	    "customerName": "John",
//	    "trackingId": "TRK123"
//	  },
//	  "receivers": [
//	    { "name": "John Doe", "email": "john@example.com", "phone": "+1234567890" }
//	  ]
//	}
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.FlowID = strings.TrimSpace(req.FlowID)
	req.ID = strings.TrimSpace(req.ID)

	if req.FlowID == "" {
		errJSON(w, http.StatusBadRequest, "flow_id is required")
		return
	}
	if len(req.Receivers) == 0 {
		errJSON(w, http.StatusBadRequest, "at least one receiver is required")
		return
	}

	// Load flow from DB
	flowRow, err := s.queries.GetFlow(r.Context(), req.FlowID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "flow not found")
			return
		}
		s.log.Error("get flow for send", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Load flow channels
	dbChannels, err := s.queries.ListFlowChannels(r.Context(), req.FlowID)
	if err != nil {
		s.log.Error("list flow channels for send", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Build flow config
	inputContract, err := flow.ParseInputContract(flowRow.InputContract)
	if err != nil {
		s.log.Error("parse input contract", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	fc := &flow.FlowConfig{
		ID:      flowRow.ID,
		Name:    flowRow.Name,
		Active:  flowRow.Active,
		Input:   inputContract,
		Channels: make([]flow.ChannelConfig, 0, len(dbChannels)),
	}

	for _, dbCh := range dbChannels {
		dc, _ := flow.ParseDefaultContent(dbCh.DefaultContent)
		rv, _ := flow.ParseRequiredVariables(dbCh.RequiredVariables)
		tpo, _ := flow.ParseTemplateParamOrder(dbCh.TemplateParamOrder)
		cc, _ := flow.ParseChannelConfig(dbCh.ChannelConfig)

		chCfg := flow.ChannelConfig{
			Channel:            dbCh.Channel,
			Enabled:            dbCh.Enabled,
			UsesTemplate:       dbCh.UsesTemplate,
			DefaultContent:     dc,
			RequiredVariables:  rv,
			TemplateParamOrder: tpo,
			ChannelConfig:      cc,
		}
		if dbCh.TemplateName.Valid {
			chCfg.TemplateName = dbCh.TemplateName.String
		}
		fc.Channels = append(fc.Channels, chCfg)
	}

	// Build send request for resolver
	sendReq := &flow.ReceiveRequest{
		FlowID:           req.FlowID,
		ID:               req.ID,
		Variables:        req.Variables,
		Content:          req.Content,
		ChannelContent:   req.ChannelContent,
		ChannelVariables: req.ChannelVariables,
		Receivers:        req.Receivers,
	}

	// Resolve
	resolved, err := flow.Resolve(fc, sendReq)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	// Dispatch
	results := make([]sendDeliveryResult, 0)
	for _, deliveries := range resolved {
		for _, del := range deliveries {
			result := s.dispatchDelivery(r.Context(), del)
			results = append(results, result)
		}
	}

	status := "completed"
	for _, r := range results {
		if r.Status == "failed" {
			status = "partial_failure"
			break
		}
	}

	respID := req.ID
	if respID == "" {
		respID = "auto"
	}

	writeJSON(w, http.StatusOK, sendResponse{
		ID:      respID,
		Status:  status,
		Results: results,
	})
}

// dispatchDelivery sends a single resolved delivery through the appropriate provider.
func (s *Server) dispatchDelivery(ctx context.Context, del flow.ResolvedDelivery) sendDeliveryResult {
	ch := del.Payload.Channel
	content := del.Payload.Content
	if content == nil {
		content = &flow.MessageContent{}
	}

	result := sendDeliveryResult{
		Receiver: del.Receiver.Name,
		Channel:  ch,
	}

	// Determine the target address based on channel
	to := ""
	switch ch {
	case "email":
		to = del.Receiver.Email
	case "sms", "whatsapp":
		to = del.Receiver.Phone
	case "fcm", "push":
		to = del.Receiver.FCMToken
	}

	if to == "" {
		result.Status = "skipped"
		result.Error = fmt.Sprintf("no %s address for receiver", ch)
		return result
	}

	// Dispatch through the provider registry
	var sendErr error
	switch ch {
	case "email":
		htmlBody := content.HTML
		textBody := content.Body
		if htmlBody == "" {
			htmlBody = textBody
		}
		_, sendErr = s.registry.DispatchEmail(ctx, to, content.Subject, htmlBody, textBody)
	case "sms":
		_, sendErr = s.registry.DispatchSMS(ctx, to, content.Body)
	case "whatsapp":
		_, sendErr = s.registry.DispatchWhatsApp(ctx, to, content.Body)
	case "fcm", "push":
		data := make(map[string]string)
		if content.Data != nil {
			for k, v := range content.Data {
				data[k] = fmt.Sprintf("%v", v)
			}
		}
		_, sendErr = s.registry.DispatchPush(ctx, to, content.Title, content.Body, data)
	default:
		sendErr = fmt.Errorf("unsupported channel: %s", ch)
	}

	if sendErr != nil {
		result.Status = "failed"
		result.Error = sendErr.Error()
		s.log.Error("dispatch failed", "channel", ch, "receiver", to, "error", sendErr)
	} else {
		result.Status = "sent"
		s.log.Info("dispatch success", "channel", ch, "receiver", to)
	}

	return result
}
