package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	getenv := func(key string) string {
		return ""
	}

	cfg, err := Load(getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("expected port 8080, got %s", cfg.Port)
	}
	if !strings.Contains(cfg.DatabaseURL, "localhost:5433") {
		t.Errorf("expected default database url, got %s", cfg.DatabaseURL)
	}
}

func TestLoad_CustomPortAndDB(t *testing.T) {
	env := map[string]string{
		"PORT":         "9090",
		"DATABASE_URL": "postgres://user:pass@remote:5432/db",
	}
	getenv := func(key string) string {
		return env[key]
	}

	cfg, err := Load(getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://user:pass@remote:5432/db" {
		t.Errorf("expected custom database url, got %s", cfg.DatabaseURL)
	}
}

func TestLoad_SMTP(t *testing.T) {
	t.Run("valid smtp", func(t *testing.T) {
		env := map[string]string{
			"SMTP_HOST":     "smtp.example.com",
			"SMTP_PORT":     "465",
			"SMTP_USERNAME": "user@example.com",
			"SMTP_PASSWORD": "secretpassword",
			"SMTP_FROM":     "noreply@example.com",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SMTP.Host != "smtp.example.com" || cfg.SMTP.Port != 465 || cfg.SMTP.Username != "user@example.com" || cfg.SMTP.Password != "secretpassword" || cfg.SMTP.From != "noreply@example.com" {
			t.Errorf("unexpected SMTP config: %+v", cfg.SMTP)
		}
	})

	t.Run("default port and from", func(t *testing.T) {
		env := map[string]string{
			"SMTP_HOST":     "smtp.example.com",
			"SMTP_USERNAME": "user@example.com",
			"SMTP_PASSWORD": "secretpassword",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SMTP.Port != 587 {
			t.Errorf("expected default port 587, got %d", cfg.SMTP.Port)
		}
		if cfg.SMTP.From != "user@example.com" {
			t.Errorf("expected default from = username, got %s", cfg.SMTP.From)
		}
	})

	t.Run("invalid port", func(t *testing.T) {
		env := map[string]string{
			"SMTP_HOST":     "smtp.example.com",
			"SMTP_PORT":     "invalid",
			"SMTP_USERNAME": "user@example.com",
			"SMTP_PASSWORD": "secretpassword",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error for invalid port, got nil")
		}
		if !strings.Contains(err.Error(), "not a valid port") {
			t.Errorf("error = %v, expected invalid port message", err)
		}
	})

	t.Run("missing username or password", func(t *testing.T) {
		env := map[string]string{
			"SMTP_HOST": "smtp.example.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "SMTP_USERNAME") {
			t.Errorf("error = %v, expected missing username/password message", err)
		}
	})

	t.Run("credentials set without host", func(t *testing.T) {
		env := map[string]string{
			"SMTP_USERNAME": "user@example.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "SMTP_HOST is missing") {
			t.Errorf("error = %v, expected missing host message", err)
		}
	})
}

func TestLoad_Resend(t *testing.T) {
	t.Run("valid resend", func(t *testing.T) {
		env := map[string]string{
			"RESEND_API_KEY": "re_123",
			"RESEND_FROM":    "sender@example.com",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Resend.APIKey != "re_123" || cfg.Resend.From != "sender@example.com" {
			t.Errorf("unexpected Resend config: %+v", cfg.Resend)
		}
	})

	t.Run("from set without api key", func(t *testing.T) {
		env := map[string]string{
			"RESEND_FROM": "sender@example.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "RESEND_API_KEY is missing") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestLoad_Twilio(t *testing.T) {
	t.Run("valid twilio sms & whatsapp & sendgrid", func(t *testing.T) {
		env := map[string]string{
			"TWILIO_ACCOUNT_SID":   "AC123",
			"TWILIO_AUTH_TOKEN":    "tok456",
			"TWILIO_SMS_FROM":      "+1234567890",
			"TWILIO_WHATSAPP_FROM": "+1098765432",
			"SENDGRID_API_KEY":     "SG.123",
			"SENDGRID_FROM":        "mail@example.com",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Twilio.AccountSID != "AC123" || cfg.Twilio.SMSFrom != "+1234567890" || cfg.Twilio.SendGridAPIKey != "SG.123" {
			t.Errorf("unexpected Twilio config: %+v", cfg.Twilio)
		}
	})

	t.Run("sid without token", func(t *testing.T) {
		env := map[string]string{
			"TWILIO_ACCOUNT_SID": "AC123",
			"TWILIO_SMS_FROM":    "+1234567890",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "TWILIO_AUTH_TOKEN is missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("token without sid", func(t *testing.T) {
		env := map[string]string{
			"TWILIO_AUTH_TOKEN": "tok123",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "TWILIO_ACCOUNT_SID is missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("sid and token without channels", func(t *testing.T) {
		env := map[string]string{
			"TWILIO_ACCOUNT_SID": "AC123",
			"TWILIO_AUTH_TOKEN":  "tok123",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no usable channel") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("sendgrid key without twilio credentials", func(t *testing.T) {
		env := map[string]string{
			"SENDGRID_API_KEY": "SG.123",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN are missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("sendgrid from without sendgrid api key", func(t *testing.T) {
		env := map[string]string{
			"SENDGRID_FROM": "from@example.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "SENDGRID_API_KEY is missing") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestLoad_Infobip(t *testing.T) {
	t.Run("valid infobip", func(t *testing.T) {
		env := map[string]string{
			"INFOBIP_API_KEY":       "ib_key",
			"INFOBIP_BASE_URL":      "https://api.infobip.com/",
			"INFOBIP_SMS_FROM":      "InfobipSMS",
			"INFOBIP_WHATSAPP_FROM": "447860099299",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Infobip.BaseURL != "https://api.infobip.com" {
			t.Errorf("expected trailing slash stripped, got %s", cfg.Infobip.BaseURL)
		}
	})

	t.Run("missing base url", func(t *testing.T) {
		env := map[string]string{
			"INFOBIP_API_KEY":  "ib_key",
			"INFOBIP_SMS_FROM": "InfobipSMS",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "INFOBIP_BASE_URL is missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("missing api key", func(t *testing.T) {
		env := map[string]string{
			"INFOBIP_BASE_URL": "https://api.infobip.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "INFOBIP_API_KEY is missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("missing channels", func(t *testing.T) {
		env := map[string]string{
			"INFOBIP_API_KEY":  "ib_key",
			"INFOBIP_BASE_URL": "https://api.infobip.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no usable channel") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestLoad_SES(t *testing.T) {
	t.Run("valid ses", func(t *testing.T) {
		env := map[string]string{
			"SES_REGION":            "us-east-1",
			"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
			"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			"SES_FROM":              "ses@example.com",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SES.Region != "us-east-1" || cfg.SES.From != "ses@example.com" {
			t.Errorf("unexpected SES config: %+v", cfg.SES)
		}
	})

	t.Run("fallback to AWS_REGION", func(t *testing.T) {
		env := map[string]string{
			"AWS_REGION":            "eu-west-1",
			"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
			"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.SES.Region != "eu-west-1" {
			t.Errorf("expected SES region eu-west-1, got %s", cfg.SES.Region)
		}
	})

	t.Run("missing secret key", func(t *testing.T) {
		env := map[string]string{
			"AWS_REGION":        "eu-west-1",
			"AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "AWS_SECRET_ACCESS_KEY is missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("missing region", func(t *testing.T) {
		env := map[string]string{
			"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
			"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no region") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("from set without aws credentials", func(t *testing.T) {
		env := map[string]string{
			"SES_FROM": "ses@example.com",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY are missing") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestLoad_FCM(t *testing.T) {
	validSA := `{
		"project_id": "my-firebase-project",
		"client_email": "firebase-adminsdk@my-firebase-project.iam.gserviceaccount.com",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7\n-----END PRIVATE KEY-----\n"
	}`

	t.Run("valid inline json", func(t *testing.T) {
		env := map[string]string{
			"FCM_CREDENTIALS_JSON": validSA,
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.FCM.ProjectID != "my-firebase-project" || cfg.FCM.ClientEmail != "firebase-adminsdk@my-firebase-project.iam.gserviceaccount.com" {
			t.Errorf("unexpected FCM config: %+v", cfg.FCM)
		}
	})

	t.Run("valid file", func(t *testing.T) {
		tmpDir := t.TempDir()
		saFile := filepath.Join(tmpDir, "service_account.json")
		if err := os.WriteFile(saFile, []byte(validSA), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		env := map[string]string{
			"FCM_CREDENTIALS_FILE": saFile,
		}
		cfg, err := Load(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.FCM.ProjectID != "my-firebase-project" {
			t.Errorf("unexpected FCM project ID: %s", cfg.FCM.ProjectID)
		}
	})

	t.Run("both inline and file set", func(t *testing.T) {
		env := map[string]string{
			"FCM_CREDENTIALS_JSON": validSA,
			"FCM_CREDENTIALS_FILE": "/path/to/file.json",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not both") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		env := map[string]string{
			"FCM_CREDENTIALS_JSON": "{invalid-json",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("missing fields in service account json", func(t *testing.T) {
		env := map[string]string{
			"FCM_CREDENTIALS_JSON": `{"project_id": "proj"}`,
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("project id alone without credential", func(t *testing.T) {
		env := map[string]string{
			"FCM_PROJECT_ID": "my-project",
		}
		_, err := Load(func(k string) string { return env[k] })
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "project id alone is not a credential") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestLoad_DefaultsField(t *testing.T) {
	env := map[string]string{
		"DEFAULT_EMAIL_PROVIDER":    "resend",
		"DEFAULT_SMS_PROVIDER":      "twilio",
		"DEFAULT_WHATSAPP_PROVIDER": "infobip",
		"DEFAULT_PUSH_PROVIDER":     "fcm",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Defaults.Email != "resend" || cfg.Defaults.SMS != "twilio" || cfg.Defaults.WhatsApp != "infobip" || cfg.Defaults.Push != "fcm" {
		t.Errorf("unexpected defaults: %+v", cfg.Defaults)
	}
}
