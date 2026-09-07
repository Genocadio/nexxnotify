package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/nexxserve/nexxnotify/internal/db"
	"github.com/nexxserve/nexxnotify/internal/flow"
)

// --- Flow CRUD ---

type flowView struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Active        bool             `json:"active"`
	InputContract flow.InputContract `json:"input"`
	Channels      []flowChannelView `json:"channels,omitempty"`
	CreatedAt     string           `json:"created_at"`
	UpdatedAt     string           `json:"updated_at"`
}

type flowChannelView struct {
	ID                 string              `json:"id"`
	Channel            string              `json:"channel"`
	Enabled            bool                `json:"enabled"`
	UsesTemplate       bool                `json:"uses_template"`
	TemplateName       string              `json:"template_name,omitempty"`
	TemplateParamOrder []string            `json:"template_param_order,omitempty"`
	DefaultContent     *flow.MessageContent `json:"default_content,omitempty"`
	RequiredVariables  []string            `json:"required_variables"`
	ChannelConfig      map[string]any      `json:"channel_config,omitempty"`
}

type createFlowReq struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Active        *bool                 `json:"active"`
	InputContract *flow.InputContract   `json:"input"`
	Channels      []createFlowChannelReq `json:"channels"`
}

type createFlowChannelReq struct {
	Channel            string                `json:"channel"`
	Enabled            *bool                 `json:"enabled"`
	UsesTemplate       *bool                 `json:"uses_template"`
	TemplateName       string                `json:"template_name"`
	TemplateParamOrder []string              `json:"template_param_order"`
	DefaultContent     *flow.MessageContent  `json:"default_content"`
	RequiredVariables  []string              `json:"required_variables"`
	ChannelConfig      map[string]any        `json:"channel_config"`
}

func (s *Server) createFlow(w http.ResponseWriter, r *http.Request) {
	var req createFlowReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)

	if req.ID == "" {
		errJSON(w, http.StatusBadRequest, "id is required")
		return
	}
	if req.Name == "" {
		errJSON(w, http.StatusBadRequest, "name is required")
		return
	}

	active := true
	if req.Active != nil {
		active = *req.Active
	}
	inputContract := flow.InputContract{}
	if req.InputContract != nil {
		inputContract = *req.InputContract
	}

	icBytes, err := json.Marshal(inputContract)
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid input contract")
		return
	}

	flowRow, err := s.queries.CreateFlow(r.Context(), db.CreateFlowParams{
		ID:            req.ID,
		Name:          req.Name,
		Active:        active,
		InputContract: icBytes,
	})
	if err != nil {
		if isUniqueViolation(err) {
			errJSON(w, http.StatusConflict, "flow with id "+req.ID+" already exists")
			return
		}
		s.log.Error("create flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Create channels
	channelViews := make([]flowChannelView, 0, len(req.Channels))
	for _, chReq := range req.Channels {
		chView, err := s.createFlowChannelInternal(r, req.ID, chReq)
		if err != nil {
			errJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		channelViews = append(channelViews, chView)
	}

	view := flowRowToView(flowRow, channelViews)
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	flowRow, err := s.queries.GetFlow(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "flow not found")
			return
		}
		s.log.Error("get flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	channels, err := s.queries.ListFlowChannels(r.Context(), id)
	if err != nil {
		s.log.Error("list flow channels", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	channelViews := make([]flowChannelView, 0, len(channels))
	for _, ch := range channels {
		channelViews = append(channelViews, flowChannelDBToView(ch))
	}

	writeJSON(w, http.StatusOK, flowRowToView(flowRow, channelViews))
}

func (s *Server) listFlows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListFlows(r.Context())
	if err != nil {
		s.log.Error("list flows", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]flowView, 0, len(rows))
	for _, f := range rows {
		channels, err := s.queries.ListFlowChannels(r.Context(), f.ID)
		if err != nil {
			s.log.Error("list flow channels", "error", err)
			continue
		}
		channelViews := make([]flowChannelView, 0, len(channels))
		for _, ch := range channels {
			channelViews = append(channelViews, flowChannelDBToView(ch))
		}
		out = append(out, flowRowToView(f, channelViews))
	}

	writeJSON(w, http.StatusOK, map[string]any{"flows": out})
}

type updateFlowReq struct {
	Name          *string              `json:"name"`
	Active        *bool                `json:"active"`
	InputContract *flow.InputContract  `json:"input"`
}

func (s *Server) updateFlow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.queries.GetFlow(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "flow not found")
			return
		}
		s.log.Error("get flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req updateFlowReq
	if !decodeJSON(w, r, &req) {
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = *req.Name
	}
	active := existing.Active
	if req.Active != nil {
		active = *req.Active
	}

	icBytes := existing.InputContract
	if req.InputContract != nil {
		icBytes, err = json.Marshal(req.InputContract)
		if err != nil {
			errJSON(w, http.StatusBadRequest, "invalid input contract")
			return
		}
	}

	err = s.queries.UpdateFlow(r.Context(), db.UpdateFlowParams{
		ID:            id,
		Name:          name,
		Active:        active,
		InputContract: icBytes,
	})
	if err != nil {
		s.log.Error("update flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	s.getFlow(w, r) // return the updated flow
}

func (s *Server) deleteFlow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	n, err := s.queries.DeleteFlow(r.Context(), id)
	if err != nil {
		s.log.Error("delete flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "flow not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Flow Channel CRUD ---

type updateFlowChannelReq struct {
	Enabled            *bool                `json:"enabled"`
	UsesTemplate       *bool                `json:"uses_template"`
	TemplateName       string               `json:"template_name"`
	TemplateParamOrder []string             `json:"template_param_order"`
	DefaultContent     *flow.MessageContent `json:"default_content"`
	RequiredVariables  []string             `json:"required_variables"`
	ChannelConfig      map[string]any       `json:"channel_config"`
}

func (s *Server) upsertFlowChannel(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "id")
	// Verify flow exists
	if _, err := s.queries.GetFlow(r.Context(), flowID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "flow not found")
			return
		}
		s.log.Error("get flow", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}

	var chReq createFlowChannelReq
	if !decodeJSON(w, r, &chReq) {
		return
	}

	chView, err := s.createFlowChannelInternal(r, flowID, chReq)
	if err != nil {
		errJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, chView)
}

func (s *Server) deleteFlowChannel(w http.ResponseWriter, r *http.Request) {
	flowID := chi.URLParam(r, "id")
	channelID := chi.URLParam(r, "channelId")

	// Verify the channel belongs to this flow
	chID, err := uuid.Parse(channelID)
	if err != nil {
		errJSON(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	ch, err := s.queries.GetFlowChannel(r.Context(), chID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(w, http.StatusNotFound, "channel not found")
			return
		}
		s.log.Error("get flow channel", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ch.FlowID != flowID {
		errJSON(w, http.StatusNotFound, "channel not found in this flow")
		return
	}

	n, err := s.queries.DeleteFlowChannel(r.Context(), chID)
	if err != nil {
		s.log.Error("delete flow channel", "error", err)
		errJSON(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		errJSON(w, http.StatusNotFound, "channel not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Helpers ---

func (s *Server) createFlowChannelInternal(r *http.Request, flowID string, chReq createFlowChannelReq) (flowChannelView, error) {
	chReq.Channel = strings.ToLower(strings.TrimSpace(chReq.Channel))
	if chReq.Channel == "" {
		return flowChannelView{}, errors.New("channel is required")
	}

	validChannels := map[string]bool{"sms": true, "email": true, "fcm": true, "whatsapp": true}
	if !validChannels[chReq.Channel] {
		return flowChannelView{}, errors.New("channel must be one of sms, email, fcm, whatsapp")
	}

	enabled := true
	if chReq.Enabled != nil {
		enabled = *chReq.Enabled
	}
	usesTemplate := false
	if chReq.UsesTemplate != nil {
		usesTemplate = *chReq.UsesTemplate
	}

	templateParamOrderBytes, _ := json.Marshal(chReq.TemplateParamOrder)
	defaultContentBytes, _ := json.Marshal(chReq.DefaultContent)
	requiredVarsBytes, _ := json.Marshal(chReq.RequiredVariables)
	channelConfigBytes, _ := json.Marshal(chReq.ChannelConfig)

	var templateName pgtype.Text
	if chReq.TemplateName != "" {
		templateName = pgtype.Text{String: chReq.TemplateName, Valid: true}
	}

	chRow, err := s.queries.UpsertFlowChannel(r.Context(), db.UpsertFlowChannelParams{
		FlowID:             flowID,
		Channel:            chReq.Channel,
		Enabled:            enabled,
		UsesTemplate:       usesTemplate,
		TemplateName:       templateName,
		TemplateParamOrder: templateParamOrderBytes,
		DefaultContent:     defaultContentBytes,
		RequiredVariables:  requiredVarsBytes,
		ChannelConfig:      channelConfigBytes,
	})
	if err != nil {
		s.log.Error("upsert flow channel", "error", err)
		return flowChannelView{}, errors.New("failed to save channel")
	}

	return flowChannelDBToView(chRow), nil
}

func flowRowToView(f db.Flow, channels []flowChannelView) flowView {
	ic, _ := flow.ParseInputContract(f.InputContract)
	return flowView{
		ID:            f.ID,
		Name:          f.Name,
		Active:        f.Active,
		InputContract: ic,
		Channels:      channels,
		CreatedAt:     f.CreatedAt.Time.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     f.UpdatedAt.Time.Format("2006-01-02T15:04:05Z"),
	}
}

func flowChannelDBToView(ch db.FlowChannel) flowChannelView {
	dc, _ := flow.ParseDefaultContent(ch.DefaultContent)
	rv, _ := flow.ParseRequiredVariables(ch.RequiredVariables)
	tpo, _ := flow.ParseTemplateParamOrder(ch.TemplateParamOrder)
	cc, _ := flow.ParseChannelConfig(ch.ChannelConfig)

	view := flowChannelView{
		ID:                 ch.ID.String(),
		Channel:            ch.Channel,
		Enabled:            ch.Enabled,
		UsesTemplate:       ch.UsesTemplate,
		DefaultContent:     dc,
		RequiredVariables:  rv,
		TemplateParamOrder: tpo,
		ChannelConfig:      cc,
	}
	if ch.TemplateName.Valid {
		view.TemplateName = ch.TemplateName.String
	}
	return view
}


