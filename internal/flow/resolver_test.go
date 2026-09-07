package flow

import (
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestValidateRequest(t *testing.T) {
	fc := &FlowConfig{
		ID:     "test-flow",
		Name:   "Test Flow",
		Active: true,
		Input: InputContract{
			AllowsContent:   true,
			AllowsVariables: true,
			Variables: map[string]VariableSchema{
				"name":    {Type: "string", Required: true},
				"orderId": {Type: "string", Required: true},
				"discount": {Type: "number", Required: false},
			},
		},
	}

	t.Run("valid request with variables", func(t *testing.T) {
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Variables: map[string]any{
				"name":    "John",
				"orderId": "123",
			},
			Receivers: []Receiver{{Name: "John", Email: "john@example.com"}},
		}
		errs := ValidateRequest(fc, req)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("missing required variable", func(t *testing.T) {
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Variables: map[string]any{
				"name": "John",
				// orderId is missing
			},
			Receivers: []Receiver{{Name: "John"}},
		}
		errs := ValidateRequest(fc, req)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "variables.orderId" {
			t.Errorf("error field = %q, want variables.orderId", errs[0].Field)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Variables: map[string]any{
				"name":    123, // should be string
				"orderId": "123",
			},
			Receivers: []Receiver{{Name: "John"}},
		}
		errs := ValidateRequest(fc, req)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Field != "variables.name" {
			t.Errorf("error field = %q, want variables.name", errs[0].Field)
		}
	})

	t.Run("content allowed passes validation", func(t *testing.T) {
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Content: &MessageContent{Body: "hello"},
			Variables: map[string]any{"name": "John", "orderId": "123"},
			Receivers: []Receiver{{Name: "John"}},
		}
		errs := ValidateRequest(fc, req)
		if len(errs) != 0 {
			t.Fatalf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("content rejected when not allowed", func(t *testing.T) {
		fc2 := &FlowConfig{
			Active: true,
			Input: InputContract{
				AllowsContent:   false,
				AllowsVariables: true,
			},
		}
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Content: &MessageContent{Body: "hello"},
			Receivers: []Receiver{{Name: "John"}},
		}
		errs := ValidateRequest(fc2, req)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
	})

	t.Run("inactive flow", func(t *testing.T) {
		fc2 := &FlowConfig{
			Active: false,
		}
		req := &ReceiveRequest{
			FlowID: "test-flow",
			Receivers: []Receiver{{Name: "John"}},
		}
		errs := ValidateRequest(fc2, req)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
	})
}

func TestResolve_DefaultContent(t *testing.T) {
	fc := &FlowConfig{
		ID:     "order-shipped",
		Name:   "Order Shipped",
		Active: true,
		Input: InputContract{
			AllowsVariables: true,
			Variables: map[string]VariableSchema{
				"trackingId": {Type: "string", Required: true},
			},
		},
		Channels: []ChannelConfig{
			{
				Channel:      "sms",
				Enabled:      true,
				UsesTemplate: false,
				DefaultContent: &MessageContent{
					Body: "Your order shipped. Track: {{trackingId}}",
				},
				RequiredVariables: []string{"trackingId"},
			},
		},
	}

	req := &ReceiveRequest{
		FlowID:    "order-shipped",
		Variables: map[string]any{"trackingId": "TRK123"},
		Receivers: []Receiver{
			{Name: "John", Phone: "+1234567890"},
		},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	key := "John"
	deliveries, ok := resolved[key]
	if !ok {
		t.Fatalf("expected deliveries for %q", key)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}

	payload := deliveries[0].Payload
	if payload.Channel != "sms" {
		t.Errorf("channel = %q, want sms", payload.Channel)
	}
	if payload.Content.Body != "Your order shipped. Track: TRK123" {
		t.Errorf("body = %q, want 'Your order shipped. Track: TRK123'", payload.Content.Body)
	}
}

func TestResolve_Template(t *testing.T) {
	fc := &FlowConfig{
		ID:     "order-shipped",
		Name:   "Order Shipped",
		Active: true,
		Input: InputContract{
			AllowsVariables: true,
			Variables: map[string]VariableSchema{
				"customerName": {Type: "string", Required: true},
				"trackingId":   {Type: "string", Required: true},
			},
		},
		Channels: []ChannelConfig{
			{
				Channel:      "whatsapp",
				Enabled:      true,
				UsesTemplate: true,
				TemplateName: "order_shipped_wa_v2",
				TemplateParamOrder: []string{"customerName", "trackingId"},
				DefaultContent: &MessageContent{
					Body: "Hi {{customerName}}, your order {{trackingId}} has shipped.",
				},
				RequiredVariables: []string{"customerName", "trackingId"},
			},
		},
	}

	req := &ReceiveRequest{
		FlowID: "order-shipped",
		Variables: map[string]any{
			"customerName": "Alice",
			"trackingId":   "TRK456",
		},
		Receivers: []Receiver{
			{Name: "Alice", Phone: "+1987654321"},
		},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	deliveries := resolved["Alice"]
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliveries))
	}

	payload := deliveries[0].Payload
	if payload.Template != "order_shipped_wa_v2" {
		t.Errorf("template = %q, want order_shipped_wa_v2", payload.Template)
	}
	if payload.Content.Body != "Hi Alice, your order TRK456 has shipped." {
		t.Errorf("body = %q, want 'Hi Alice, your order TRK456 has shipped.'", payload.Content.Body)
	}
}

func TestResolve_RawContent(t *testing.T) {
	fc := &FlowConfig{
		ID:     "announcement",
		Name:   "System Announcement",
		Active: true,
		Input: InputContract{
			AllowsContent: true,
		},
		Channels: []ChannelConfig{
			{Channel: "sms", Enabled: true},
			{Channel: "email", Enabled: true},
		},
	}

	req := &ReceiveRequest{
		FlowID: "announcement",
		Content: &MessageContent{
			Body:    "System maintenance at 2am",
			Subject: "Maintenance Notice",
		},
		Receivers: []Receiver{
			{Name: "Bob", Phone: "+1111111111", Email: "bob@example.com"},
		},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	deliveries := resolved["Bob"]
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliveries))
	}

	channels := map[string]bool{}
	for _, d := range deliveries {
		channels[d.Payload.Channel] = true
	}
	if !channels["sms"] {
		t.Error("expected sms delivery")
	}
	if !channels["email"] {
		t.Error("expected email delivery")
	}
}

func TestResolve_MultipleReceivers(t *testing.T) {
	fc := &FlowConfig{
		ID:     "otp",
		Name:   "OTP",
		Active: true,
		Input: InputContract{
			AllowsVariables: true,
			Variables: map[string]VariableSchema{
				"code": {Type: "string", Required: true},
			},
		},
		Channels: []ChannelConfig{
			{
				Channel:        "sms",
				Enabled:        true,
				DefaultContent: &MessageContent{Body: "Your OTP: {{code}}"},
			},
		},
	}

	req := &ReceiveRequest{
		FlowID:    "otp",
		Variables: map[string]any{"code": "456789"},
		Receivers: []Receiver{
			{Name: "Alice", Phone: "+111"},
			{Name: "Bob", Phone: "+222"},
		},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(resolved) != 2 {
		t.Fatalf("expected 2 receiver groups, got %d", len(resolved))
	}

	for _, deliveries := range resolved {
		if len(deliveries) != 1 {
			t.Errorf("expected 1 delivery per receiver, got %d", len(deliveries))
		}
		if deliveries[0].Payload.Content.Body != "Your OTP: 456789" {
			t.Errorf("body = %q, want 'Your OTP: 456789'", deliveries[0].Payload.Content.Body)
		}
	}
}

func TestResolve_ChannelNarrowing(t *testing.T) {
	fc := &FlowConfig{
		ID:     "mixed",
		Name:   "Mixed",
		Active: true,
		Input: InputContract{
			AllowsContent: true,
		},
		Channels: []ChannelConfig{
			{Channel: "sms", Enabled: true},
			{Channel: "email", Enabled: true},
			{Channel: "fcm", Enabled: true},
		},
	}

	req := &ReceiveRequest{
		FlowID:  "mixed",
		Content: &MessageContent{Body: "Hello", Title: "Hello"},
		Receivers: []Receiver{
			{Name: "Eve", Phone: "+111", Email: "eve@example.com", FCMToken: "tok123"},
		},
	}

	// Sender restricts to only email
	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Should get all 3 channels since receiver has all contact info
	deliveries := resolved["Eve"]
	if len(deliveries) != 3 {
		t.Errorf("expected 3 deliveries, got %d", len(deliveries))
	}
}

func TestResolve_NoChannelContent(t *testing.T) {
	fc := &FlowConfig{
		ID:     "test",
		Name:   "Test",
		Active: true,
		Input:  InputContract{},
		Channels: []ChannelConfig{
			{Channel: "sms", Enabled: true},
		},
	}

	req := &ReceiveRequest{
		FlowID:    "test",
		Receivers: []Receiver{{Name: "NoPhone", Email: "x@x.com"}},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Receiver has no phone, so no SMS delivery
	if len(resolved) != 0 {
		t.Errorf("expected 0 deliveries (no phone for SMS), got %d", len(resolved))
	}
}

func TestResolve_ChannelContentOverride(t *testing.T) {
	fc := &FlowConfig{
		ID:     "test",
		Name:   "Test",
		Active: true,
		Input: InputContract{
			AllowsContent: true,
		},
		Channels: []ChannelConfig{
			{
				Channel:        "sms",
				Enabled:        true,
				DefaultContent: &MessageContent{Body: "Default body"},
			},
		},
	}

	req := &ReceiveRequest{
		FlowID:  "test",
		Content: &MessageContent{Body: "Request body"},
		ChannelContent: map[string]*MessageContent{
			"sms": {Body: "Override body"},
		},
		Receivers: []Receiver{{Name: "Test", Phone: "+111"}},
	}

	resolved, err := Resolve(fc, req)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	body := resolved["Test"][0].Payload.Content.Body
	if body != "Override body" {
		t.Errorf("body = %q, want 'Override body' (channel override)", body)
	}
}

func TestResolve_InactiveFlow(t *testing.T) {
	fc := &FlowConfig{
		ID:     "inactive",
		Active: false,
	}

	req := &ReceiveRequest{
		FlowID:    "inactive",
		Receivers: []Receiver{{Name: "Test"}},
	}

	_, err := Resolve(fc, req)
	if err == nil {
		t.Error("expected error for inactive flow")
	}
}

func TestPickChannels_ReceiverCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		receiver Receiver
		channels []ChannelConfig
		expected int
	}{
		{
			name:     "email only receiver gets email channel",
			receiver: Receiver{Email: "a@b.com"},
			channels: []ChannelConfig{
				{Channel: "email", Enabled: true},
				{Channel: "sms", Enabled: true},
			},
			expected: 1,
		},
		{
			name:     "phone only receiver gets sms and whatsapp",
			receiver: Receiver{Phone: "+111"},
			channels: []ChannelConfig{
				{Channel: "email", Enabled: true},
				{Channel: "sms", Enabled: true},
				{Channel: "whatsapp", Enabled: true},
			},
			expected: 2,
		},
		{
			name:     "full receiver gets all",
			receiver: Receiver{Email: "a@b.com", Phone: "+111", FCMToken: "tok"},
			channels: []ChannelConfig{
				{Channel: "email", Enabled: true},
				{Channel: "sms", Enabled: true},
				{Channel: "whatsapp", Enabled: true},
				{Channel: "fcm", Enabled: true},
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pickChannels(tt.receiver, tt.channels, nil)
			if len(result) != tt.expected {
				t.Errorf("pickChannels() returned %d channels, want %d", len(result), tt.expected)
			}
		})
	}
}
