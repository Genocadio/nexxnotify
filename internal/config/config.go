package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type ResendConfig struct {
	APIKey string
	From   string
}

type TwilioConfig struct {
	AccountSID     string
	AuthToken      string
	SMSFrom        string
	WhatsAppFrom   string
	SendGridAPIKey string
	SendGridFrom   string
}

type InfobipConfig struct {
	APIKey       string
	BaseURL      string
	SMSFrom      string
	WhatsAppFrom string
}

type SESConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	From            string
}

type FCMConfig struct {
	ProjectID   string
	ClientEmail string
	Credentials string
}

type Defaults struct {
	Email    string
	SMS      string
	WhatsApp string
	Push     string
}

type Config struct {
	Port        string
	DatabaseURL string
	SMTP        SMTPConfig
	Resend      ResendConfig
	Twilio      TwilioConfig
	Infobip     InfobipConfig
	SES         SESConfig
	FCM         FCMConfig
	Defaults    Defaults
}

type Getenv func(string) string

func Load(getenv Getenv) (*Config, error) {
	cfg := &Config{
		Port:        envOr(getenv, "PORT", "8080"),
		DatabaseURL: envOr(getenv, "DATABASE_URL", "postgres://nexxnotify:nexxnotify@localhost:5433/nexxnotify?sslmode=disable"),
	}

	var errs []string

	cfg.SMTP = loadSMTP(getenv, &errs)
	cfg.Resend = loadResend(getenv, &errs)
	cfg.Twilio = loadTwilio(getenv, &errs)
	cfg.Infobip = loadInfobip(getenv, &errs)
	cfg.SES = loadSES(getenv, &errs)
	cfg.FCM = loadFCM(getenv, &errs)
	cfg.Defaults = Defaults{
		Email:    strings.TrimSpace(getenv("DEFAULT_EMAIL_PROVIDER")),
		SMS:      strings.TrimSpace(getenv("DEFAULT_SMS_PROVIDER")),
		WhatsApp: strings.TrimSpace(getenv("DEFAULT_WHATSAPP_PROVIDER")),
		Push:     strings.TrimSpace(getenv("DEFAULT_PUSH_PROVIDER")),
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func loadSMTP(getenv Getenv, errs *[]string) SMTPConfig {
	host := strings.TrimSpace(getenv("SMTP_HOST"))
	username := strings.TrimSpace(getenv("SMTP_USERNAME"))
	password := getenv("SMTP_PASSWORD")
	from := strings.TrimSpace(getenv("SMTP_FROM"))

	if host == "" {
		if username != "" || password != "" || from != "" {
			*errs = append(*errs, "SMTP_USERNAME/SMTP_PASSWORD/SMTP_FROM set but SMTP_HOST is missing")
		}
		return SMTPConfig{}
	}

	var missing []string
	if username == "" {
		missing = append(missing, "SMTP_USERNAME")
	}
	if password == "" {
		missing = append(missing, "SMTP_PASSWORD")
	}
	if len(missing) > 0 {
		*errs = append(*errs, fmt.Sprintf("SMTP_HOST is set but %s is missing", strings.Join(missing, " and ")))
	}

	portStr := envOr(getenv, "SMTP_PORT", "587")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		*errs = append(*errs, fmt.Sprintf("SMTP_PORT %q is not a valid port", portStr))
		port = 0
	}

	if from == "" {
		from = username
	}

	return SMTPConfig{Host: host, Port: port, Username: username, Password: password, From: from}
}

func loadResend(getenv Getenv, errs *[]string) ResendConfig {
	apiKey := strings.TrimSpace(getenv("RESEND_API_KEY"))
	from := strings.TrimSpace(getenv("RESEND_FROM"))

	if apiKey == "" && from != "" {
		*errs = append(*errs, "RESEND_FROM is set but RESEND_API_KEY is missing")
	}
	return ResendConfig{APIKey: apiKey, From: from}
}

func loadTwilio(getenv Getenv, errs *[]string) TwilioConfig {
	sid := strings.TrimSpace(getenv("TWILIO_ACCOUNT_SID"))
	token := getenv("TWILIO_AUTH_TOKEN")
	smsFrom := strings.TrimSpace(getenv("TWILIO_SMS_FROM"))
	whatsappFrom := strings.TrimSpace(getenv("TWILIO_WHATSAPP_FROM"))
	sendgridKey := getenv("SENDGRID_API_KEY")
	sendgridFrom := strings.TrimSpace(getenv("SENDGRID_FROM"))

	switch {
	case sid != "" && token == "":
		*errs = append(*errs, "TWILIO_ACCOUNT_SID is set but TWILIO_AUTH_TOKEN is missing")
	case sid == "" && token != "":
		*errs = append(*errs, "TWILIO_AUTH_TOKEN is set but TWILIO_ACCOUNT_SID is missing")
	}

	if sid != "" && smsFrom == "" && whatsappFrom == "" && sendgridKey == "" {
		*errs = append(*errs, "TWILIO_ACCOUNT_SID is set but no usable channel: set at least one of TWILIO_SMS_FROM, TWILIO_WHATSAPP_FROM or SENDGRID_API_KEY")
	}

	if sendgridKey != "" && sid == "" {
		*errs = append(*errs, "SENDGRID_API_KEY is set but TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN are missing")
	}
	if sendgridKey == "" && sendgridFrom != "" {
		*errs = append(*errs, "SENDGRID_FROM is set but SENDGRID_API_KEY is missing")
	}

	return TwilioConfig{
		AccountSID:     sid,
		AuthToken:      token,
		SMSFrom:        smsFrom,
		WhatsAppFrom:   whatsappFrom,
		SendGridAPIKey: sendgridKey,
		SendGridFrom:   sendgridFrom,
	}
}

func loadInfobip(getenv Getenv, errs *[]string) InfobipConfig {
	apiKey := getenv("INFOBIP_API_KEY")
	baseURL := strings.TrimRight(strings.TrimSpace(getenv("INFOBIP_BASE_URL")), "/")
	smsFrom := strings.TrimSpace(getenv("INFOBIP_SMS_FROM"))
	whatsappFrom := strings.TrimSpace(getenv("INFOBIP_WHATSAPP_FROM"))

	switch {
	case apiKey != "" && baseURL == "":
		*errs = append(*errs, "INFOBIP_API_KEY is set but INFOBIP_BASE_URL is missing")
	case apiKey == "" && baseURL != "":
		*errs = append(*errs, "INFOBIP_BASE_URL is set but INFOBIP_API_KEY is missing")
	}
	if apiKey != "" && smsFrom == "" && whatsappFrom == "" {
		*errs = append(*errs, "INFOBIP_API_KEY is set but no usable channel: set INFOBIP_SMS_FROM and/or INFOBIP_WHATSAPP_FROM")
	}

	return InfobipConfig{APIKey: apiKey, BaseURL: baseURL, SMSFrom: smsFrom, WhatsAppFrom: whatsappFrom}
}

func loadSES(getenv Getenv, errs *[]string) SESConfig {
	region := strings.TrimSpace(getenv("SES_REGION"))
	if region == "" {
		region = strings.TrimSpace(getenv("AWS_REGION"))
	}
	accessKeyID := getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := getenv("AWS_SECRET_ACCESS_KEY")
	from := strings.TrimSpace(getenv("SES_FROM"))

	if accessKeyID != "" && secretAccessKey == "" {
		*errs = append(*errs, "AWS_ACCESS_KEY_ID is set but AWS_SECRET_ACCESS_KEY is missing")
	}
	if accessKeyID == "" && secretAccessKey != "" {
		*errs = append(*errs, "AWS_SECRET_ACCESS_KEY is set but AWS_ACCESS_KEY_ID is missing")
	}
	if accessKeyID != "" && region == "" {
		*errs = append(*errs, "AWS credentials are set but no region: set SES_REGION or AWS_REGION")
	}
	if from != "" && accessKeyID == "" {
		*errs = append(*errs, "SES_FROM is set but AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY are missing")
	}

	return SESConfig{Region: region, AccessKeyID: accessKeyID, SecretAccessKey: secretAccessKey, From: from}
}

type serviceAccountFile struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

func loadFCM(getenv Getenv, errs *[]string) FCMConfig {
	inline := strings.TrimSpace(getenv("FCM_CREDENTIALS_JSON"))
	file := getenv("FCM_CREDENTIALS_FILE")

	if inline != "" && file != "" {
		*errs = append(*errs, "set either FCM_CREDENTIALS_JSON or FCM_CREDENTIALS_FILE, not both")
		return FCMConfig{}
	}

	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			*errs = append(*errs, fmt.Sprintf("FCM_CREDENTIALS_FILE %q cannot be read: %v", file, err))
			return FCMConfig{}
		}
		inline = string(data)
	}

	if inline == "" {
		if getenv("FCM_PROJECT_ID") != "" || getenv("FCM_CLIENT_EMAIL") != "" {
			*errs = append(*errs, "FCM_CREDENTIALS_JSON/FCM_CREDENTIALS_FILE is required (a project id alone is not a credential)")
		}
		return FCMConfig{}
	}

	var sa serviceAccountFile
	if err := json.Unmarshal([]byte(inline), &sa); err != nil {
		*errs = append(*errs, fmt.Sprintf("FCM_CREDENTIALS_JSON is not valid JSON: %v", err))
		return FCMConfig{}
	}
	var missing []string
	if sa.ProjectID == "" {
		missing = append(missing, "project_id")
	}
	if sa.ClientEmail == "" {
		missing = append(missing, "client_email")
	}
	if sa.PrivateKey == "" {
		missing = append(missing, "private_key")
	}
	if len(missing) > 0 {
		*errs = append(*errs, fmt.Sprintf("FCM_CREDENTIALS_JSON does not look like a Firebase service account key: missing %s", strings.Join(missing, ", ")))
		return FCMConfig{}
	}

	return FCMConfig{ProjectID: sa.ProjectID, ClientEmail: sa.ClientEmail, Credentials: inline}
}

func envOr(getenv Getenv, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}
