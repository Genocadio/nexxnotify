package flow

import (
	"fmt"
	"sort"
)

// ChannelPayload is a fully resolved payload ready to be sent on one channel
// to one receiver. The dispatch layer uses this to call the appropriate provider.
type ChannelPayload struct {
	Channel   string          `json:"channel"`
	Content   *MessageContent `json:"content"`
	Variables map[string]any  `json:"variables,omitempty"` // merged variables for template rendering
	Template  string          `json:"template,omitempty"`  // template name, if uses_template
}

// Validation error types.

// ValidationError describes why a send request was rejected.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateRequest checks a send request against the flow's input contract.
// Returns a list of validation errors (empty = valid).
func ValidateRequest(fc *FlowConfig, req *ReceiveRequest) []ValidationError {
	var errs []ValidationError

	if !fc.Active {
		errs = append(errs, ValidationError{Field: "flow", Message: "flow is not active"})
	}

	// Check content permission
	if req.Content != nil && !fc.Input.AllowsContent {
		errs = append(errs, ValidationError{Field: "content", Message: "flow does not allow raw content"})
	}

	// Check variables permission
	if len(req.Variables) > 0 && !fc.Input.AllowsVariables {
		errs = append(errs, ValidationError{Field: "variables", Message: "flow does not allow variables"})
	}

	// Validate variable types and required constraints
	if fc.Input.AllowsVariables && len(fc.Input.Variables) > 0 {
		for name, schema := range fc.Input.Variables {
			val, present := req.Variables[name]
			if schema.Required && !present {
				errs = append(errs, ValidationError{
					Field:   "variables." + name,
					Message: fmt.Sprintf("required variable %q is missing", name),
				})
				continue
			}
			if present {
				if ok := validateType(val, schema.Type); !ok {
					errs = append(errs, ValidationError{
						Field:   "variables." + name,
						Message: fmt.Sprintf("variable %q expected type %s", name, schema.Type),
					})
				}
			}
		}
	}

	return errs
}

func validateType(val any, expected string) bool {
	if val == nil {
		return true // nil is acceptable; required check handles absence
	}
	switch expected {
	case "string":
		_, ok := val.(string)
		return ok
	case "number":
		switch val.(type) {
		case float64, int, int64:
			return true
		}
		return false
	case "boolean":
		_, ok := val.(bool)
		return ok
	default:
		return true // unknown type, accept
	}
}

// Resolve sends a flow configuration and a send request through the resolver,
// producing a list of (receiver, channel, payload) tuples ready for dispatch.
// It handles:
//   - Template rendering (named or positional param mapping)
//   - Default content with variable interpolation
//   - Raw content passthrough (content-mode flows)
//   - Channel-specific overrides (channelContent, channelVariables)
//   - Channel narrowing (req.Channels)
func Resolve(fc *FlowConfig, req *ReceiveRequest) (map[string][]ResolvedDelivery, error) {
	// Validate input
	if errs := ValidateRequest(fc, req); len(errs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", errs)
	}

	// Build target channel list (enabled channels only)
	targetChannels := make([]ChannelConfig, 0)
	for _, ch := range fc.Channels {
		if ch.Enabled {
			targetChannels = append(targetChannels, ch)
		}
	}

	// Sort for deterministic output
	sort.Slice(targetChannels, func(i, j int) bool {
		return targetChannels[i].Channel < targetChannels[j].Channel
	})

	results := make(map[string][]ResolvedDelivery)

	for _, receiver := range req.Receivers {
		eligibleChannels := pickChannels(receiver, targetChannels, req)

		for _, ch := range eligibleChannels {
			payload := renderPayload(ch, req)
			results[receiverKey(receiver)] = append(results[receiverKey(receiver)], ResolvedDelivery{
				Receiver: receiver,
				Payload:  payload,
			})
		}
	}

	return results, nil
}

// ResolvedDelivery pairs a receiver with a channel payload.
type ResolvedDelivery struct {
	Receiver Receiver
	Payload  ChannelPayload
}

// pickChannels determines which channels a receiver can receive on, based on
// their available contact info (email for email, phone for sms/whatsapp, etc.).
func pickChannels(receiver Receiver, channels []ChannelConfig, req *ReceiveRequest) []ChannelConfig {
	var eligible []ChannelConfig
	for _, ch := range channels {
		// Check if the receiver has the right contact info for this channel
		if !receiverCanReceive(receiver, ch.Channel) {
			continue
		}
		eligible = append(eligible, ch)
	}
	return eligible
}

// receiverCanReceive checks if a receiver has the contact info needed for a channel.
func receiverCanReceive(receiver Receiver, channel string) bool {
	switch channel {
	case "email":
		return receiver.Email != ""
	case "sms":
		return receiver.Phone != ""
	case "whatsapp":
		return receiver.Phone != ""
	case "fcm":
		return receiver.FCMToken != ""
	default:
		return false
	}
}

// renderPayload produces a ChannelPayload for a single channel, applying
// template rendering, default content interpolation, or raw content passthrough.
func renderPayload(ch ChannelConfig, req *ReceiveRequest) ChannelPayload {
	// 1. Per-channel content override (bypasses template/default entirely)
	if req.ChannelContent != nil {
		if override, ok := req.ChannelContent[ch.Channel]; ok && override != nil {
			return ChannelPayload{
				Channel: ch.Channel,
				Content: override,
			}
		}
	}

	// Merge base + channel-specific variables
	chVars := req.Variables
	if req.ChannelVariables != nil {
		if extra, ok := req.ChannelVariables[ch.Channel]; ok {
			chVars = MergeVariables(req.Variables, extra)
		}
	}

	// 2. Template rendering
	if ch.UsesTemplate && ch.TemplateName != "" {
		return renderTemplatePayload(ch, chVars, req)
	}

	// 3. Raw content passthrough (content-mode flow)
	if req.Content != nil {
		content := InterpolateContent(req.Content, chVars)
		return ChannelPayload{
			Channel:   ch.Channel,
			Content:   content,
			Variables: chVars,
		}
	}

	// 4. Default content with variable interpolation
	if ch.DefaultContent != nil {
		content := InterpolateContent(ch.DefaultContent, chVars)
		return ChannelPayload{
			Channel:   ch.Channel,
			Content:   content,
			Variables: chVars,
		}
	}

	// No content available — empty payload
	return ChannelPayload{
		Channel:   ch.Channel,
		Content:   &MessageContent{},
		Variables: chVars,
	}
}

// renderTemplatePayload renders a template-based channel payload.
func renderTemplatePayload(ch ChannelConfig, variables map[string]any, req *ReceiveRequest) ChannelPayload {
	content := &MessageContent{}

	if ch.DefaultContent != nil {
		// Use default content as the base, interpolate variables into it
		content = InterpolateContent(ch.DefaultContent, variables)
	} else if req.Content != nil {
		// Fallback: use request content if no default content
		content = InterpolateContent(req.Content, variables)
	}

	// For template-based channels, include the template name in the payload
	// so the dispatch layer knows to use template rendering on the provider side
	payload := ChannelPayload{
		Channel:   ch.Channel,
		Content:   content,
		Variables: variables,
		Template:  ch.TemplateName,
	}

	// For positional param mapping, render positional placeholders
	if len(ch.TemplateParamOrder) > 0 {
		if content.Body != "" {
			content.Body = RenderPositional(content.Body, ch.TemplateParamOrder, variables)
		}
		if content.Subject != "" {
			content.Subject = RenderPositional(content.Subject, ch.TemplateParamOrder, variables)
		}
		if content.Title != "" {
			content.Title = RenderPositional(content.Title, ch.TemplateParamOrder, variables)
		}
	}

	return payload
}

// receiverKey returns a string key for a receiver (for grouping results).
func receiverKey(r Receiver) string {
	if r.Name != "" {
		return r.Name
	}
	if r.Email != "" {
		return r.Email
	}
	if r.Phone != "" {
		return r.Phone
	}
	return "unknown"
}
