package flow

import "encoding/json"

// InputContract defines what a trigger is allowed to send when firing a flow.
type InputContract struct {
	AllowsContent   bool                           `json:"allows_content"`
	AllowsVariables bool                           `json:"allows_variables"`
	Variables        map[string]VariableSchema      `json:"variables,omitempty"`
}

// VariableSchema describes a single variable in the input contract.
type VariableSchema struct {
	Type        string `json:"type"`                   // "string", "number", "boolean"
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// MessageContent represents raw, ad-hoc content for a notification.
// The fields present depend on the channel: SMS uses Body, Email uses
// Subject+Body (+HTML), FCM uses Title+Body.
type MessageContent struct {
	Body    string            `json:"body,omitempty"`
	Subject string            `json:"subject,omitempty"`   // email
	Title   string            `json:"title,omitempty"`     // fcm
	HTML    string            `json:"html,omitempty"`      // email
	Data    map[string]any    `json:"data,omitempty"`      // fcm data payload
}

// Receiver represents an end-user who will receive the message.
// The system picks channels based on which fields are populated.
type Receiver struct {
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	FCMToken   string `json:"fcm_token,omitempty"`   // optional, for push
}

// ReceiveRequest is the payload sent to POST /send to trigger a flow.
type ReceiveRequest struct {
	FlowID          string                    `json:"flow_id"`
	ID              string                    `json:"id"`                // idempotency key
	Variables       map[string]any            `json:"variables,omitempty"`
	Content         *MessageContent           `json:"content,omitempty"`
	ChannelContent  map[string]*MessageContent `json:"channel_content,omitempty"` // per-channel override
	ChannelVariables map[string]map[string]any `json:"channel_variables,omitempty"`
	Receivers       []Receiver                `json:"receivers"`
}

// SendResult captures the outcome of a single channel send for one receiver.
type SendResult struct {
	Receiver string `json:"receiver"` // name or email/phone
	Channel  string `json:"channel"`
	Status   string `json:"status"` // "sent" or "failed"
	Error    string `json:"error,omitempty"`
}

// FlowConfig is a fully resolved flow with its channels loaded from the DB.
type FlowConfig struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Active   bool           `json:"active"`
	Input    InputContract  `json:"input"`
	Channels []ChannelConfig `json:"channels"`
}

// ChannelConfig represents one channel's configuration within a flow.
type ChannelConfig struct {
	Channel            string              `json:"channel"`
	Enabled            bool                `json:"enabled"`
	UsesTemplate       bool                `json:"uses_template"`
	TemplateName       string              `json:"template_name,omitempty"`
	TemplateParamOrder []string            `json:"template_param_order,omitempty"`
	DefaultContent     *MessageContent     `json:"default_content,omitempty"`
	RequiredVariables  []string            `json:"required_variables"`
	ChannelConfig      map[string]any      `json:"channel_config,omitempty"`
}

// ParseInputContract deserializes JSONB input_contract from the DB.
func ParseInputContract(raw []byte) (InputContract, error) {
	var ic InputContract
	if len(raw) == 0 {
		return ic, nil
	}
	err := json.Unmarshal(raw, &ic)
	return ic, err
}

// ParseDefaultContent deserializes JSONB default_content from the DB.
func ParseDefaultContent(raw []byte) (*MessageContent, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var mc MessageContent
	err := json.Unmarshal(raw, &mc)
	return &mc, err
}

// ParseRequiredVariables deserializes JSONB required_variables from the DB.
func ParseRequiredVariables(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var vars []string
	err := json.Unmarshal(raw, &vars)
	return vars, err
}

// ParseTemplateParamOrder deserializes JSONB template_param_order from the DB.
func ParseTemplateParamOrder(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var order []string
	err := json.Unmarshal(raw, &order)
	return order, err
}

// ParseChannelConfig deserializes JSONB channel_config from the DB.
func ParseChannelConfig(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var cfg map[string]any
	err := json.Unmarshal(raw, &cfg)
	return cfg, err
}
