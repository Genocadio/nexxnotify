package provider

import (
	"context"
	"testing"
)

type mockMultiSender struct {
	name      string
	smsSent   int
	waSent    int
	pushSent  int
	emailSent int
}

func (m *mockMultiSender) Name() string { return m.name }
func (m *mockMultiSender) Channels() []ChannelStatus {
	return []ChannelStatus{
		{Channel: Email, Configured: true},
		{Channel: SMS, Configured: true},
		{Channel: WhatsApp, Configured: true},
		{Channel: Push, Configured: true},
	}
}

func (m *mockMultiSender) SendEmail(ctx context.Context, msg EmailMessage) (SendResult, error) {
	m.emailSent++
	return SendResult{OK: true, Message: "email-" + m.name}, nil
}

func (m *mockMultiSender) SendSMS(ctx context.Context, msg SMSMessage) (SendResult, error) {
	m.smsSent++
	return SendResult{OK: true, Message: "sms-" + m.name}, nil
}

func (m *mockMultiSender) SendWhatsApp(ctx context.Context, msg SMSMessage) (SendResult, error) {
	m.waSent++
	return SendResult{OK: true, Message: "wa-" + m.name}, nil
}

func (m *mockMultiSender) SendPush(ctx context.Context, msg PushMessage) (SendResult, error) {
	m.pushSent++
	return SendResult{OK: true, Message: "push-" + m.name}, nil
}

func TestDispatchMethods(t *testing.T) {
	mock := &mockMultiSender{name: "all_in_one"}
	reg := NewRegistry([]Provider{mock}, nil)

	ctx := context.Background()

	// 1. DispatchEmail
	res, err := reg.DispatchEmail(ctx, "to@example.com", "Subj", "<p>HTML</p>", "Text")
	if err != nil || !res.OK || mock.emailSent != 1 {
		t.Fatalf("DispatchEmail failed: res=%+v err=%v", res, err)
	}

	// 2. DispatchSMS
	res, err = reg.DispatchSMS(ctx, "+1234567890", "sms body")
	if err != nil || !res.OK || mock.smsSent != 1 {
		t.Fatalf("DispatchSMS failed: res=%+v err=%v", res, err)
	}

	// 3. DispatchWhatsApp
	res, err = reg.DispatchWhatsApp(ctx, "+1234567890", "wa body")
	if err != nil || !res.OK || mock.waSent != 1 {
		t.Fatalf("DispatchWhatsApp failed: res=%+v err=%v", res, err)
	}

	// 4. DispatchPush
	res, err = reg.DispatchPush(ctx, "token123", "Alert", "push body", map[string]string{"k": "v"})
	if err != nil || !res.OK || mock.pushSent != 1 {
		t.Fatalf("DispatchPush failed: res=%+v err=%v", res, err)
	}

	// 5. General Dispatch router
	_, err = reg.Dispatch(ctx, "email", "to@example.com", "Subj", "Title", "Text", "HTML", nil)
	if err != nil || mock.emailSent != 2 {
		t.Errorf("Dispatch email failed: %v", err)
	}

	_, err = reg.Dispatch(ctx, "sms", "+123", "", "", "sms text", "", nil)
	if err != nil || mock.smsSent != 2 {
		t.Errorf("Dispatch sms failed: %v", err)
	}

	_, err = reg.Dispatch(ctx, "whatsapp", "+123", "", "", "wa text", "", nil)
	if err != nil || mock.waSent != 2 {
		t.Errorf("Dispatch whatsapp failed: %v", err)
	}

	_, err = reg.Dispatch(ctx, "fcm", "tok", "", "Title", "push text", "", nil)
	if err != nil || mock.pushSent != 2 {
		t.Errorf("Dispatch fcm failed: %v", err)
	}

	_, err = reg.Dispatch(ctx, "unsupported", "target", "", "", "", "", nil)
	if err == nil {
		t.Error("expected error for unsupported channel, got nil")
	}
}

func TestRegistry_Lookups(t *testing.T) {
	mock1 := &mockMultiSender{name: "prov1"}
	mock2 := &mockMultiSender{name: "prov2"}

	reg := NewRegistry([]Provider{mock1, mock2}, map[Channel]string{
		SMS: "prov2",
	})

	if len(reg.Providers()) != 2 {
		t.Errorf("expected 2 providers, got %d", len(reg.Providers()))
	}

	p, ok := reg.ByName("prov1")
	if !ok || p.Name() != "prov1" {
		t.Errorf("ByName prov1 failed")
	}

	_, ok = reg.ByName("nonexistent")
	if ok {
		t.Errorf("expected false for nonexistent provider")
	}

	def, ok := reg.Default(SMS)
	if !ok || def != "prov2" {
		t.Errorf("expected default SMS provider = prov2, got %s", def)
	}

	if !reg.IsDefault("prov2", SMS) {
		t.Errorf("expected IsDefault(prov2, SMS) to be true")
	}
	if reg.IsDefault("prov1", SMS) {
		t.Errorf("expected IsDefault(prov1, SMS) to be false")
	}
}
