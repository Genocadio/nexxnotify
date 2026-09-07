# Flows & Sending — API Guide

This guide covers how to create Flows, configure channels (SMS, Email, FCM, WhatsApp), set up templates or default content, and send messages to receivers.

---

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Flow** | A reusable messaging configuration. Defines an input contract (what triggers can send) and channel composition (how each channel renders content). |
| **Channel** | A delivery mechanism: `sms`, `email`, `fcm` (push), or `whatsapp`. Each channel in a flow can be independently enabled/disabled. |
| **Template** | An optional named template for a channel. Variables are interpolated via `{{variableName}}` placeholders. |
| **Default Content** | Static content with optional `{{variable}}` placeholders. Used when `uses_template` is false. |
| **Receiver** | An end-user who receives the message. The system routes to the correct channel based on which contact fields (email, phone, fcm_token) are populated. |
| **Send Request** | Triggers a flow with receivers and optional variables/content. |

---

## 1. Creating a Flow

### Endpoint

```
POST /flows
```

### Request Body

```json
{
  "id": "flow_order_shipped",
  "name": "Order Shipped",
  "active": true,
  "input": {
    "allows_content": false,
    "allows_variables": true,
    "variables": {
      "customerName": { "type": "string", "required": true },
      "trackingId": { "type": "string", "required": true },
      "eta": { "type": "string", "required": false }
    }
  },
  "channels": [
    {
      "channel": "sms",
      "enabled": true,
      "uses_template": false,
      "default_content": {
        "body": "Your order has shipped. Track: {{trackingId}}"
      },
      "required_variables": ["trackingId"]
    },
    {
      "channel": "email",
      "enabled": true,
      "uses_template": true,
      "template_name": "order_shipped_email",
      "template_param_order": ["customerName", "trackingId", "eta"],
      "default_content": {
        "subject": "Your order {{trackingId}} has shipped",
        "body": "Hi {{customerName}}, your order has shipped. ETA: {{eta}}"
      },
      "required_variables": ["customerName", "trackingId", "eta"]
    }
  ]
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string (required) | Unique identifier for the flow |
| `name` | string (required) | Human-readable name |
| `active` | boolean | Whether the flow is active (default: `true`) |
| `input` | object | Input contract — what triggers are allowed to send |
| `channels` | array | Channel configurations for this flow |

#### Input Contract

| Field | Type | Description |
|-------|------|-------------|
| `allows_content` | boolean | If `true`, triggers can send raw `content` (body, subject, title, HTML) |
| `allows_variables` | boolean | If `true`, triggers can send `variables` for template interpolation |
| `variables` | object | Schema defining allowed variables. Each key is a variable name with `type` (string/number/boolean), `required` (boolean), and optional `description` |

#### Channel Configuration

| Field | Type | Description |
|-------|------|-------------|
| `channel` | string | One of: `sms`, `email`, `fcm`, `whatsapp` |
| `enabled` | boolean | Whether this channel is active |
| `uses_template` | boolean | If `true`, the channel uses a named template |
| `template_name` | string | Template name (for provider-side templates like WhatsApp) |
| `template_param_order` | string[] | Ordered variable names for positional rendering (WhatsApp-style `{{1}}`, `{{2}}`) |
| `default_content` | object | Static content with `{{variable}}` placeholders |
| `required_variables` | string[] | Variables that must be present for this channel to send |
| `channel_config` | object | Provider-specific config (sender ID, from-number, etc.) |

### Response (201 Created)

```json
{
  "id": "flow_order_shipped",
  "name": "Order Shipped",
  "active": true,
  "input": { ... },
  "channels": [ ... ],
  "created_at": "2026-09-02T10:00:00Z",
  "updated_at": "2026-09-02T10:00:00Z"
}
```

---

## 2. Managing Flow Channels

Channels can be added, updated, or removed from an existing flow.

### Add/Update a Channel (Upsert)

```
POST /flows/{flow_id}/channels
```

```json
{
  "channel": "fcm",
  "enabled": true,
  "uses_template": false,
  "default_content": {
    "title": "Order Shipped",
    "body": "Track your order: {{trackingId}}"
  },
  "required_variables": ["trackingId"]
}
```

If a channel of the same type already exists on the flow, it is updated. Otherwise, it is created.

### Delete a Channel

```
DELETE /flows/{flow_id}/channels/{channel_id}
```

---

## 3. Flow CRUD

### Get a Flow

```
GET /flows/{id}
```

### List All Flows

```
GET /flows
```

### Update a Flow

```
PUT /flows/{id}
```

```json
{
  "name": "Order Shipped v2",
  "active": true,
  "input": {
    "allows_content": false,
    "allows_variables": true,
    "variables": {
      "customerName": { "type": "string", "required": true },
      "trackingId": { "type": "string", "required": true }
    }
  }
}
```

Only provided fields are updated; omitted fields keep their existing values.

### Delete a Flow

```
DELETE /flows/{id}
```

Cascades to delete all associated flow channels.

---

## 4. Sending Messages

### Endpoint

```
POST /send
```

### Request Body

```json
{
  "flow_id": "flow_order_shipped",
  "id": "ntf_abc123",
  "variables": {
    "customerName": "John",
    "trackingId": "TRK123",
    "eta": "Sep 5"
  },
  "receivers": [
    {
      "name": "John Doe",
      "email": "john@example.com",
      "phone": "+1234567890"
    }
  ]
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `flow_id` | string (required) | The flow to trigger |
| `id` | string | Idempotency key for this send |
| `variables` | object | Variable values for template interpolation |
| `content` | object | Raw content (only if `input.allows_content` is true) |
| `channel_content` | object | Per-channel content override, bypassing templates/defaults |
| `channel_variables` | object | Per-channel variable additions |
| `receivers` | array (required) | At least one receiver |

#### Receiver Object

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Receiver's display name |
| `email` | string | Email address (enables email channel) |
| `phone` | string | Phone number (enables SMS and WhatsApp channels) |
| `fcm_token` | string | FCM registration token (enables push channel) |

The system automatically routes to the correct channels based on which contact fields are populated. A receiver with both `email` and `phone` will receive on both channels.

### Response

```json
{
  "id": "ntf_abc123",
  "status": "completed",
  "results": [
    {
      "receiver": "John Doe",
      "channel": "email",
      "status": "sent"
    },
    {
      "receiver": "John Doe",
      "channel": "sms",
      "status": "sent"
    }
  ]
}
```

Status values: `completed` (all sent), `partial_failure` (some failed).

Individual result statuses: `sent`, `failed`, `skipped` (no contact info for channel).

---

## 5. Content Modes

### Mode 1: Variables Only (Template/Default Content)

The flow uses `allows_variables: true` and channels have either `uses_template: true` (named template) or `uses_template: false` with `default_content`.

```json
// Flow config
{
  "input": { "allows_variables": true, "allows_content": false },
  "channels": [{
    "channel": "sms",
    "uses_template": false,
    "default_content": { "body": "Hi {{name}}, your code is {{code}}" }
  }]
}

// Send request
{
  "flow_id": "otp_flow",
  "variables": { "name": "Alice", "code": "456789" },
  "receivers": [{ "name": "Alice", "phone": "+1234567890" }]
}
```

**Result:** SMS body becomes `"Hi Alice, your code is 456789"`.

### Mode 2: Raw Content (No Templates)

The flow uses `allows_content: true`. The caller provides the full content.

```json
// Flow config
{
  "input": { "allows_content": true, "allows_variables": false },
  "channels": [
    { "channel": "sms", "enabled": true },
    { "channel": "email", "enabled": true }
  ]
}

// Send request
{
  "flow_id": "announcement",
  "content": {
    "body": "System maintenance at 2am UTC",
    "subject": "Maintenance Notice",
    "title": "Maintenance"
  },
  "receivers": [
    { "name": "Bob", "phone": "+1111111111", "email": "bob@example.com" }
  ]
}
```

**Result:**
- SMS gets `body`: `"System maintenance at 2am UTC"`
- Email gets `subject` + `body`: `"Maintenance Notice"` + `"System maintenance at 2am UTC"`

### Mode 3: Mixed (Content + Variables)

The flow allows both. Variables are interpolated into the provided content.

```json
// Send request
{
  "flow_id": "marketing",
  "variables": { "name": "John", "discount": 20 },
  "content": {
    "body": "Hey {{name}}, you get {{discount}}% off!"
  },
  "receivers": [{ "name": "John", "email": "john@example.com" }]
}
```

**Result:** Email body becomes `"Hey John, you get 20% off!"`.

### Mode 4: Channel Content Override

Override a specific channel's content for a single send, bypassing templates/defaults.

```json
{
  "flow_id": "order_shipped",
  "variables": { "customerName": "John", "trackingId": "TRK123" },
  "channel_content": {
    "email": {
      "subject": "Custom note from support",
      "body": "Hey John, quick update on your order..."
    }
  },
  "receivers": [{ "name": "John", "email": "john@example.com", "phone": "+123" }]
}
```

**Result:** Email uses the custom content. SMS still uses the flow's default content with variable interpolation.

---

## 6. Per-Channel Template Behavior

### SMS

- No provider-side template registry
- `default_content.body` with `{{variable}}` placeholders
- Character limit consideration (160 GSM-7 / 70 UCS-2 per segment)

```json
{
  "channel": "sms",
  "uses_template": false,
  "default_content": {
    "body": "Your OTP is {{code}}. Valid for 5 minutes."
  },
  "required_variables": ["code"]
}
```

### Email

- Supports `subject`, `body`, and `html` fields
- Can use provider-side templates (e.g., SendGrid dynamic templates) via `template_name`
- Named variable interpolation (variables are passed by name)

```json
{
  "channel": "email",
  "uses_template": true,
  "template_name": "sendgrid_template_id_123",
  "template_param_order": ["name", "orderId"],
  "default_content": {
    "subject": "Order {{orderId}} Confirmation",
    "body": "Hi {{name}}, your order is confirmed."
  },
  "required_variables": ["name", "orderId"]
}
```

### WhatsApp

- Requires pre-approved Meta templates
- Positional variable mapping (`{{1}}`, `{{2}}`) via `template_param_order`
- Template name must match Meta-approved name exactly

```json
{
  "channel": "whatsapp",
  "uses_template": true,
  "template_name": "order_update_v2",
  "template_param_order": ["customerName", "trackingId"],
  "required_variables": ["customerName", "trackingId"]
}
```

### FCM (Push)

- No provider-side template — always sends fully composed payload
- Uses `title` and `body` from content
- Can include a `data` payload for app-side handling

```json
{
  "channel": "fcm",
  "uses_template": false,
  "default_content": {
    "title": "Order Update",
    "body": "Your order {{trackingId}} has shipped!"
  },
  "required_variables": ["trackingId"]
}
```

---

## 7. Validation Rules

The send request is validated against the flow's input contract before any channel work:

1. **Flow must be active** — inactive flows reject all sends
2. **Content permission** — if `allows_content: false`, sending `content` is rejected
3. **Variables permission** — if `allows_variables: false`, sending `variables` is rejected
4. **Required variables** — all variables marked `required: true` in the input contract must be present
5. **Type checking** — variables must match their declared type (string, number, boolean)
6. **Per-channel required variables** — each channel's `required_variables` must be satisfied
7. **Receiver eligibility** — a receiver can only receive on channels for which they have contact info (email → email, phone → sms/whatsapp, fcm_token → fcm)

---

## 8. Environment Variables for Providers

For channels to actually send, the corresponding provider must be configured via environment variables:

| Channel | Provider | Required Env Vars |
|---------|----------|-------------------|
| Email | Resend | `RESEND_API_KEY`, `RESEND_FROM` |
| Email | SMTP | `SMTP_HOST`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` |
| Email | SES | `SES_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `SES_FROM` |
| SMS | Twilio | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_SMS_FROM` |
| WhatsApp | Twilio | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_WHATSAPP_FROM` |
| SMS/WhatsApp | Infobip | `INFOBIP_API_KEY`, `INFOBIP_BASE_URL`, `INFOBIP_SMS_FROM` |
| Push | FCM | `FCM_CREDENTIALS_JSON` or `FCM_CREDENTIALS_FILE` |

Set `DEFAULT_EMAIL_PROVIDER`, `DEFAULT_SMS_PROVIDER`, `DEFAULT_WHATSAPP_PROVIDER`, `DEFAULT_PUSH_PROVIDER` to choose which provider handles each channel.

---

## 9. Example: Full End-to-End Flow

### Step 1: Create the Flow

```bash
curl -X POST http://localhost:8080/flows \
  -H "Content-Type: application/json" \
  -d '{
    "id": "otp_send",
    "name": "OTP Send",
    "input": {
      "allows_variables": true,
      "variables": {
        "code": { "type": "string", "required": true }
      }
    },
    "channels": [
      {
        "channel": "sms",
        "uses_template": false,
        "default_content": { "body": "Your verification code is {{code}}" },
        "required_variables": ["code"]
      },
      {
        "channel": "email",
        "uses_template": false,
        "default_content": {
          "subject": "Your Verification Code",
          "body": "Your code is {{code}}"
        },
        "required_variables": ["code"]
      }
    ]
  }'
```

### Step 2: Send a Message

```bash
curl -X POST http://localhost:8080/send \
  -H "Content-Type: application/json" \
  -d '{
    "flow_id": "otp_send",
    "id": "otp_user123",
    "variables": { "code": "847291" },
    "receivers": [
      { "name": "Alice", "email": "alice@example.com", "phone": "+1234567890" }
    ]
  }'
```

### Step 3: Response

```json
{
  "id": "otp_user123",
  "status": "completed",
  "results": [
    { "receiver": "Alice", "channel": "email", "status": "sent" },
    { "receiver": "Alice", "channel": "sms", "status": "sent" }
  ]
}
```

---

## 10. Flow State Diagram

```
┌─────────────────────────────────────────┐
│              CREATE FLOW                │
│  (id, name, input contract, channels)   │
└──────────────────┬──────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│           CONFIGURE CHANNELS             │
│  POST /flows/{id}/channels              │
│  - SMS:   default content + variables    │
│  - Email: template or default content    │
│  - FCM:   title + body with variables   │
│  - WhatsApp: named template + params     │
└──────────────────┬───────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│            TRIGGER (SEND)                │
│  POST /send                             │
│  - flow_id + variables + receivers       │
│  - System picks channels per receiver    │
│  - Interpolates variables / uses content │
│  - Dispatches to configured providers    │
└──────────────────┬───────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│          DISPATCH RESULTS                │
│  - Each receiver gets channel payloads   │
│  - Provider sends via configured API     │
│  - Results returned per receiver/channel │
└──────────────────────────────────────────┘
```
