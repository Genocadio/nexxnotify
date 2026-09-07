package flow

import (
	"testing"
)

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		vars     map[string]any
		expected string
	}{
		{
			name:     "basic replacement",
			text:     "Hello {{name}}, your order {{orderId}} shipped",
			vars:     map[string]any{"name": "John", "orderId": "12345"},
			expected: "Hello John, your order 12345 shipped",
		},
		{
			name:     "unknown placeholder left unchanged",
			text:     "Hello {{name}}, code: {{unknown}}",
			vars:     map[string]any{"name": "John"},
			expected: "Hello John, code: {{unknown}}",
		},
		{
			name:     "empty variables",
			text:     "Hello {{name}}",
			vars:     nil,
			expected: "Hello {{name}}",
		},
		{
			name:     "no placeholders",
			text:     "Hello World",
			vars:     map[string]any{"name": "John"},
			expected: "Hello World",
		},
		{
			name:     "numeric value",
			text:     "You have {{count}} items",
			vars:     map[string]any{"count": 42},
			expected: "You have 42 items",
		},
		{
			name:     "boolean value",
			text:     "Active: {{active}}",
			vars:     map[string]any{"active": true},
			expected: "Active: true",
		},
		{
			name:     "multiple same placeholder",
			text:     "{{greet}} {{name}}, {{greet}} again",
			vars:     map[string]any{"greet": "Hi", "name": "Bob"},
			expected: "Hi Bob, Hi again",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Interpolate(tt.text, tt.vars)
			if got != tt.expected {
				t.Errorf("Interpolate() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestInterpolateContent(t *testing.T) {
	content := &MessageContent{
		Subject: "Order {{orderId}}",
		Body:    "Hi {{name}}, your order has shipped.",
		Title:   "Shipped: {{orderId}}",
	}
	vars := map[string]any{"name": "Alice", "orderId": "TRK999"}

	got := InterpolateContent(content, vars)

	if got.Subject != "Order TRK999" {
		t.Errorf("Subject = %q, want %q", got.Subject, "Order TRK999")
	}
	if got.Body != "Hi Alice, your order has shipped." {
		t.Errorf("Body = %q, want %q", got.Body, "Hi Alice, your order has shipped.")
	}
	if got.Title != "Shipped: TRK999" {
		t.Errorf("Title = %q, want %q", got.Title, "Shipped: TRK999")
	}
}

func TestInterpolateContent_Nil(t *testing.T) {
	got := InterpolateContent(nil, nil)
	if got != nil {
		t.Errorf("InterpolateContent(nil) = %v, want nil", got)
	}
}

func TestMissingVariables(t *testing.T) {
	required := []string{"name", "orderId", "trackingUrl"}
	vars := map[string]any{"name": "John"}

	missing := MissingVariables(required, vars)

	if len(missing) != 2 {
		t.Fatalf("expected 2 missing, got %d: %v", len(missing), missing)
	}
	if missing[0] != "orderId" {
		t.Errorf("missing[0] = %q, want %q", missing[0], "orderId")
	}
	if missing[1] != "trackingUrl" {
		t.Errorf("missing[1] = %q, want %q", missing[1], "trackingUrl")
	}
}

func TestMissingVariables_AllPresent(t *testing.T) {
	required := []string{"name", "orderId"}
	vars := map[string]any{"name": "John", "orderId": "123"}

	missing := MissingVariables(required, vars)

	if len(missing) != 0 {
		t.Errorf("expected 0 missing, got %d: %v", len(missing), missing)
	}
}

func TestMergeVariables(t *testing.T) {
	base := map[string]any{"name": "John", "count": 5}
	extra := map[string]any{"count": 10, "extra": "val"}

	merged := MergeVariables(base, extra)

	if merged["name"] != "John" {
		t.Errorf("merged[name] = %v, want John", merged["name"])
	}
	if merged["count"] != 10 {
		t.Errorf("merged[count] = %v, want 10 (channel override)", merged["count"])
	}
	if merged["extra"] != "val" {
		t.Errorf("merged[extra] = %v, want val", merged["extra"])
	}
}

func TestMergeVariables_Nils(t *testing.T) {
	got := MergeVariables(nil, nil)
	if got != nil {
		t.Errorf("MergeVariables(nil, nil) = %v, want nil", got)
	}
}

func TestRenderPositional(t *testing.T) {
	body := "Hi {{1}}, your order {{2}} is on the way."
	paramOrder := []string{"customerName", "trackingId"}
	vars := map[string]any{"customerName": "Alice", "trackingId": "TRK42"}

	got := RenderPositional(body, paramOrder, vars)
	expected := "Hi Alice, your order TRK42 is on the way."

	if got != expected {
		t.Errorf("RenderPositional() = %q, want %q", got, expected)
	}
}
