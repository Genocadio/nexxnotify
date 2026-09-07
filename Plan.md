# Notification Platform — Build Plan

From tenant creation → multi-channel delivery → workflows/waits → broadcasting → extensible channels.

---

## 0. Design Goals

- **Multi-tenant**: every tenant is isolated (config, data, rate limits, credentials).
- **Channel-agnostic core**: the engine never hardcodes "email" or "sms" — channels are plugins registered against an interface. Adding a new channel = writing an adapter, not touching the engine.
- **Any service can trigger a notification**: a stable public API (`POST /notifications`, `POST /broadcasts`, `POST /events`) that internal or external services call. Auth via API key / tenant token.
- **Durable, non-blocking waits**: workflows can pause for events or timeouts without holding a worker/thread.
- **Broadcast = fan-out of the same primitives**: no separate engine for "send to 1" vs "send to 2 million."

---

## 1. Hierarchical Entity Model

Each level below only depends on levels above it. Nothing at a lower level ever hardcodes something from a level above — resolution always flows top-down at request time. Broadcasting builds on top of this whole stack but is intentionally **not** covered here yet.

```
Tenant
  └── Tenant API Key            (auth INTO the platform)
  └── Recipient                  (tenant's end-user + their per-channel identities)
  └── Channel Type               (platform-level: push/email/sms/...)
        └── Channel Provider     (platform-level: fcm/smtp/ses/twilio/vonage/...)
              └── Channel Config (PLATFORM default, or TENANT-owned)
        └── Tenant Channel Setting  (which config a tenant actually uses)
  └── Template                   (per tenant, per notification type, per channel)
  └── Workflow                   (per tenant, references Channel Types — never Providers — + Templates)
        └── Notification         (one request, references a Workflow; idempotency-keyed)
              └── Execution      (one live run of that Workflow)
                    └── Delivery (one attempt, one channel, one RESOLVED provider config)
              └── Event          (resumes a waiting Execution)
```

---

### 1.1 Tenant

**Purpose:** root of isolation. Every other entity is scoped by `tenant_id`.

**Properties**

```
Tenant
-----------------
id
name
status              # active | suspended
created_at
default_locale
```

**Workflow at this level**

```
Tenant created
   ↓
gets access to ALL platform Channel Types immediately
   (defaulted to PLATFORM channel configs — zero setup)
   ↓
can optionally: issue API keys, override channel configs,
                 create templates, create workflows
```

---

### 1.2 Tenant API Key

**Purpose:** authenticates requests _into_ the platform. Unrelated to channel provider credentials (1.4) — do not conflate the two.

**Properties**

```
TenantApiKey
-----------------
id
tenant_id
key_prefix          # shown in UI, e.g. "tk_live_9f2a"
key_hash             # hashed, raw key shown once at creation only
scopes                # ["notifications:write", "events:write", ...]
environment           # live | test
status                 # active | revoked
expires_at             # nullable
last_used_at
```

**Workflow at this level**

```
POST /tenants/:id/api-keys → raw key returned once
      ↓
every inbound request: Authorization: Bearer <key>
      ↓
hash key → lookup TenantApiKey → resolve tenant_id + scopes
      ↓
attach to request context → all downstream lookups scoped to this tenant
```

---

### 1.3 Recipient / Identity

**Purpose:** the tenant's end-user. This is a first-class model, not a stray field on Notification — it's what makes "goal + preferred channel" resolution (1.8) actually work, since eligibility depends on _which identities exist, are verified, and haven't opted out_.

**Properties**

```
Recipient
-----------------
id
tenant_id
external_ref          # tenant's own user ID, for their reference
display_name
locale
created_at
```

**Channel Identity** — one per address/token per channel, not folded into Recipient itself (a recipient can have multiple emails, multiple push tokens across devices, etc.)

```
ChannelIdentity
-----------------
id
recipient_id
channel_type              # "push" | "email" | "sms" | "whatsapp" | ...
value                        # email address / phone number / push token / webhook URL
verified                       # bool
is_primary                       # if multiple identities exist for one channel_type
status                             # active | invalid | bounced
```

**Channel Preference** — opt-in/opt-out control, separate from identity (an identity can exist and be verified but still be opted out).

```
ChannelPreference
-----------------
recipient_id
channel_type
notification_type       # optional — allow per-type opt-out (e.g. opt out of "marketing" but not "otp")
opted_in                  # bool, default true for transactional types
```

**Consent** — audit trail of when/how opt-in or opt-out happened. Needed for compliance, not just runtime logic.

```
Consent
-----------------
recipient_id
channel_type
action              # OPT_IN | OPT_OUT
source                # "api" | "unsubscribe_link" | "sms_stop" | "admin"
occurred_at
```

**Workflow at this level**

```
Recipient created (via API or first Notification referencing an unknown external_ref)
      ↓
ChannelIdentity added per channel (email, phone, push token...)
      ↓
identity verification (optional per channel — e.g. email double opt-in, SMS OTP)
      ↓
at NOTIFY-step channel eligibility check (1.8):
   does recipient have a verified, active ChannelIdentity for this channel_type?
   is ChannelPreference.opted_in true for this channel_type + notification_type?
      ↓
   only eligible channels enter the workflow's "preferred channel" resolution
```

This is what "is the recipient reachable" / "has the user opted out" actually resolve against — earlier sections referenced these checks informally; this is where they live.

---

### 1.4 Channel Type / Provider / Config

This is a three-layer split — don't collapse it into one table, each layer changes independently.

**Channel Type** — the category. Platform-defined, not per-tenant.

```
ChannelType
-----------------
id
name                 # "push" | "email" | "sms" | "whatsapp" | ...
```

**Channel Provider** — an implementation of a Channel Type. Platform-defined (you write the adapter code); tenants never create these, they just use them.

```
ChannelProvider
-----------------
id
channel_type_id
name                  # "fcm" | "smtp" | "ses" | "twilio" | "vonage"
adapter_key            # maps to the plugin/adapter implementation
```

**Channel Config** — actual credentials for a Provider. Can be owned by the platform (shared default) or a specific tenant (their own account). A tenant can have **more than one active `ChannelConfig` per `channel_type`** — e.g. their own Twilio _and_ their own Vonage account both configured for `sms`.

```
ChannelConfig
-----------------
id
provider_id
owner_type             # PLATFORM | TENANT
tenant_id               # null when owner_type = PLATFORM
label                    # e.g. "Twilio - primary", "Vonage - backup"
credentials                # encrypted at rest
is_active
rate_limit                 # e.g. {value: 500, per: "second"} — the actual provider-side quota
                              for THIS config (Twilio's limit differs from Vonage's, a tenant's own
                              account differs from the platform's shared account)
```

**Tenant Channel Provider Setting** — for a given tenant + channel type, which config(s) are enabled, which one is the **default**, and the fallback order among the rest. This replaces a single on/off toggle: a tenant can mix platform-default and their-own configs side by side for the same channel type.

```
TenantChannelProviderSetting
-----------------
id
tenant_id
channel_type_id
channel_config_id      # one row per enabled config for this tenant+channel_type
is_default               # exactly one TRUE per (tenant_id, channel_type_id)
priority                   # fallback order when is_default's provider fails; lower = tried sooner
enabled
```

**Workflow at this level**

```
Tenant created
      ↓
auto-seeded: one TenantChannelProviderSetting per ChannelType,
             pointing at the PLATFORM default ChannelConfig,
             is_default = true, priority = 0
             (zero-setup — every channel type works immediately)
      ↓
tenant optionally: Settings → Channels → SMS → "Connect your own Twilio"
      ↓
ChannelConfig(owner_type=TENANT, provider=twilio) created
      ↓
tenant chooses: "Make this my default for SMS"
      ↓
new TenantChannelProviderSetting row: is_default=true, priority=0
existing PLATFORM-default row: is_default=false, priority pushed down (becomes fallback)
      ↓
tenant can repeat for a second provider (e.g. Vonage) → becomes priority=1 fallback,
   or another provider entirely, without ever touching the SMS default
      ↓
resolveConfig(tenant_id, channel_type) called at send time:
   → return the ordered list of enabled ChannelConfigs for this tenant+channel_type,
     is_default first, then by priority ascending
```

**Default provider selection UI, conceptually**

```
Tenant → Settings → Channels → SMS
   [x] Twilio (your account)        ← default
   [ ] Vonage (your account)        ← fallback #1
   [ ] Platform default (shared)    ← fallback #2, always available as last resort
```

A tenant can always leave the platform default enabled as a final fallback even after connecting their own providers — this is a per-tenant choice, not automatic, since some tenants may not want traffic silently falling back onto shared platform credentials.

**Runtime failover across providers of the same channel type**

```
email
 ├── SMTP
 ├── SES
 ├── SendGrid
 └── Mailgun

sms
 ├── Twilio
 ├── Vonage
 ├── Africa's Talking
 └── local provider
```

At send time, `resolveConfig()` returns the tenant's ordered list from `TenantChannelProviderSetting` (default first, then fallback priority). The engine walks that list:

```
try default ChannelConfig → provider unhealthy/failed → try next in priority list
   → only if ALL enabled configs for this channel_type fail does the
     Execution treat the channel itself as failed and move to the next
     workflow step (e.g. sms → email fallback)
```

**Rate limiting lives in the Channel Resolver, not as a separate bolted-on system**

Rate limiting isn't a generic per-tenant knob applied after the fact — it's a property of _which config is about to be used_, so it belongs in the same resolution step as everything else in 1.4:

```
resolveConfig(tenant_id, channel_type):
   candidates = ordered list of enabled ChannelConfigs (default first, then priority)
      ↓
   for each candidate:
        check token bucket / leaky bucket for THIS channel_config_id
        (bucket refill rate = candidate.rate_limit)
             ↓
        capacity available? → return this candidate for use
        capacity exhausted?  → try next candidate in the list
                                (a provider being rate-limited is treated the
                                 same as a provider being unhealthy — failover applies)
      ↓
   all candidates exhausted/rate-limited → Delivery queues/backs off, does NOT
   silently drop; Execution's normal retry_policy (1.8) governs what happens next
```

This means:

- **Per-config limits, not one global number** — a tenant's own Twilio account has a different quota than the platform's shared Twilio account, and both differ from their Vonage fallback. Each `ChannelConfig.rate_limit` is independent.
- **Rate-limit exhaustion is just another failover trigger** — it reuses the exact same "try next candidate" path as a provider outage, no separate code path.
- **Broadcast throttling (Phase 9) composes with this, doesn't replace it** — a broadcast's own dispatch pacing controls how fast notifications are _created_; the Channel Resolver's per-config limit is the hard ceiling on how fast any single provider is actually called, whether traffic comes from a broadcast or from many individual sends at once.

**Hard rule: workflows reference `channel_type`, never a specific `provider`.**

```
Bad:   { "channel": "twilio" }
Good:  { "channel": "sms" }
```

Provider selection (including failover across providers of the same type) is entirely the Channel Resolver's job (1.4), invisible to the Workflow definition (1.6). This is what makes swapping or adding SMS providers a zero-downtime, zero-workflow-change operation.

---

### 1.5 Template

**Purpose:** per-channel content for a given notification type, so one notification can render differently per channel from the same underlying data. Managed centrally, same pattern as Channel Config (1.4): a **PLATFORM default** exists for every `(notification_type, channel)` pair, and a **tenant can override it with their own**, per channel, independently.

**Properties**

```
Template
-----------------
id
owner_type                # PLATFORM | TENANT
tenant_id                  # null when owner_type = PLATFORM
notification_type           # "otp", "payment_receipt", "order_shipped"...
channel                       # "push" | "email" | "sms" | "whatsapp" | ...
format                          # PLAIN_TEXT | HTML | STRUCTURED   (see per-channel note below)
subject                           # email-only
body                                # {{variable}} placeholders — meaning depends on `format`
variables_schema                     # [{name, required, type}, ...]
locale                                 # default "en"
version                                  # incremented on edit
status                                     # DRAFT | PUBLISHED
```

```
TenantTemplateSetting
-----------------
tenant_id
notification_type
channel
mode                # USE_DEFAULT | USE_OWN
template_id          # set only when mode = USE_OWN
```

**Resolution — identical shape to resolveConfig, now resolveTemplate**

```
resolveTemplate(tenant_id, notification_type, channel):

   setting = TenantTemplateSetting.find(tenant_id, notification_type, channel)

   if setting is null OR setting.mode == "USE_DEFAULT":
        template = Template.find(
            owner_type = "PLATFORM",
            notification_type = notification_type,
            channel = channel
        )

   if setting.mode == "USE_OWN":
        template = Template.find(id = setting.template_id)

   return { template_id: template.id, version: template.version }
```

`resolveTemplate` is only ever called **once**, at Notification creation — not again at send time. See "Template versioning" below for why.

**Workflow at this level**

```
Platform ships PLATFORM templates for its built-in notification_types + channels
      ↓
every tenant instantly has working templates (e.g. default "otp" for push/sms/email)
      ↓
tenant optionally: Settings → Templates → "otp" → email → "Build our own"
      ↓
Template(owner_type=TENANT) created + validated + published
TenantTemplateSetting updated → mode=USE_OWN
      ↓
at Notification creation time, per channel the workflow might use:
   resolveTemplate(...) → {template_id, version} pinned onto the Notification (see 1.7)
      ↓
at send time, per channel: render(pinned_template.body, notification.data)
   — uses the PINNED version, not whatever the template looks like right now
```

**Template versioning — pin at creation, don't re-resolve at send time**

A workflow can sit in `WAITING` for minutes or days (e.g. a 5-minute OTP retry chain, or a multi-day approval flow). If the tenant edits their "otp" email template while an Execution is mid-flight, that in-flight Execution must **not** suddenly render the new template — otherwise you get the same class of bug workflow-versioning already solves for step logic (1.6), just for content instead. So:

```
Notification.resolved_templates:      # populated once, at creation
   { "push": {template_id: "t_1", version: 3},
     "email": {template_id: "t_9", version: 7},
     "sms": {template_id: "t_2", version: 1} }
```

Every NOTIFY step, for every channel, renders against this pinned map — never a fresh `resolveTemplate()` call. Editing a template only affects **new** Notifications created after the edit, exactly like editing a Workflow only affects new Executions.

**Per-channel authoring complexity — not all channels are equal**

|Channel|`format`|Authoring complexity|
|---|---|---|
|SMS|`PLAIN_TEXT`|Trivial — plain string, character-count validation, no rich formatting.|
|Push|`PLAIN_TEXT` / `STRUCTURED`|Small — title + body strings, maybe an image URL and a deep-link.|
|WhatsApp|`STRUCTURED`|Moderate — provider-approved template shape, limited variable placement.|
|**Email**|`HTML`|**The hard one** — needs a real editor experience, not just a text box.|

Because email is qualitatively harder, give it its own sub-model rather than forcing it through the same plain-text `body` field as everything else:

```
EmailTemplateBody
-----------------
template_id
html                  # rendered HTML, or generated from a structured design (blocks/MJML)
plain_text_fallback     # auto-generated or manual, for clients that don't render HTML
preheader                 # inbox preview text
attachments_allowed          # bool / list of allowed types
```

Practical build options for the email authoring side (pick one, don't build all three):

- **Raw HTML editor** — tenant pastes/writes HTML directly. Fastest to build, worst tenant UX.
- **MJML / block-based builder** — tenant composes from structured blocks (header, button, text, image), you compile to HTML server-side. Best long-term UX, more upfront build.
- **Plain-text-only email fallback** — for tenants who don't need branded email, treat it like SMS (subject + plain body), skip rich formatting entirely as an MVP.

Recommendation: ship email with the **plain-text-only fallback** first (keeps Phase 3 of the build plan simple and unblocks everything else), then layer in HTML/MJML as a later, isolated enhancement — it doesn't touch templates for any other channel.

Notifications may skip templates entirely with `content_mode = RAW`, providing final per-channel content directly (see 1.7) — including raw HTML for email in that mode.

---

### 1.6 Workflow

**Purpose:** tenant-defined logic — which channels to try, in what order, what to wait for, what happens on timeout/failure. Independent of any specific notification instance.

**Properties**

```
Workflow
-----------------
id
tenant_id
name
notification_type          # links workflow to the notification types it handles
version                      # incremented on edit; running Executions pin to their version
steps                          # ordered/branching step definitions:
   step:
     type            # NOTIFY | WAIT | CALL_API | CONDITION
     channel_goal     # explicit channel OR goal + preferred channel list
     on_success        # next step
     on_failure          # next step (fallback)
     on_timeout            # next step
     retry_policy            # attempts, backoff
     wait_for                  # event type + duration (WAIT steps only)
```

**Workflow at this level (meta, but literally how it behaves)**

```
Tenant defines Workflow (steps, waits, fallbacks)
      ↓
referenced by future Notifications via workflow_id
      ↓
editing a Workflow → creates a new version
      ↓
running Executions keep using the version they started on
new Notifications use the latest version
```

---

### 1.7 Notification

**Purpose:** a single request to notify one recipient, using one Workflow.

**Properties**

```
Notification
-----------------
id
tenant_id
idempotency_key       # from caller's Idempotency-Key header, unique per (tenant_id, recipient_id)
recipient_id
notification_type
workflow_id
content_mode          # TEMPLATE | RAW
data                    # used when content_mode = TEMPLATE
content                  # used when content_mode = RAW: { channel: {subject?, body} }
resolved_templates        # used when content_mode = TEMPLATE: { channel: {template_id, version} },
                             # pinned once at creation — see Template versioning (1.5)
status                     # PENDING | RUNNING | COMPLETED | FAILED | EXPIRED | CANCELLED
created_at
```

**Workflow at this level**

```
POST /notifications
Idempotency-Key: <caller-supplied key>
      ↓
lookup existing Notification WHERE tenant_id + recipient_id + idempotency_key
      ↓ found                              ↓ not found
return existing Notification          proceed to create
(no duplicate created, no duplicate      ↓
 Execution started — this is why it's   validate: workflow exists, is active
 NOT a Phase-12 hardening feature —     validate: if content_mode = TEMPLATE →
 a caller retry after a timeout must        every channel the workflow *might* use has a Template
 never double-send an OTP or SMS)               → resolveTemplate() per channel, pin into resolved_templates
                                         if content_mode = RAW →
                                             every channel the workflow *might* use has `content[channel]`
                                        ↓ (fail fast here, not mid-execution)
                                        Notification created, status = PENDING
                                        ↓
                                        Execution created from Notification.workflow_id (see 1.8)
```

`idempotency_key` needs a unique constraint on `(tenant_id, recipient_id, idempotency_key)` at the DB level — the lookup-then-create must be atomic (constraint violation → treat as "found", return existing) or a race between two concurrent identical requests will still slip through. Scoping to `recipient_id` too (not just `tenant_id`) matters: a tenant might reuse the same key across a batch (e.g. `order_id` as the key for an "order shipped" notification sent to multiple people on the same order) — without `recipient_id` in the constraint, the second recipient's notification would silently collapse into the first's and never send.

---

### 1.8 Execution

**Purpose:** the live, stateful run of a Workflow against a specific Notification. This is what actually moves through steps, waits, and resumes.

**Properties**

```
Execution
-----------------
id
notification_id
workflow_id
workflow_version        # pinned at creation
current_step
status                    # RUNNING | WAITING | COMPLETED | FAILED | EXPIRED
waiting_for                # event type, when status = WAITING
timeout_at                   # when status = WAITING
context                        # accumulated state (e.g. which channels already tried)
```

**Workflow at this level**

```
load Execution → evaluate current_step
      ↓
NOTIFY step:
   resolve channel (explicit or goal-based eligibility check)
   resolve content  = resolveContent(notification, channel)   [1.5/1.7]
                       — uses notification.resolved_templates[channel] (pinned at creation),
                         never a fresh resolveTemplate() call, so mid-flight template edits
                         never change an in-progress Execution's rendered content
   resolve config    = resolveConfig(tenant_id, channel)        [1.4]
   → create Delivery (1.9)
      ↓
WAIT step:
   persist status=WAITING, waiting_for, timeout_at → exit (non-blocking)
   resumed later by Event (1.10) or scheduler timeout sweep
      ↓
on resume: advance to on_event/on_timeout step → loop
      ↓
eventually reaches COMPLETED | FAILED | EXPIRED
```

---

### 1.9 Delivery

**Purpose:** one concrete attempt to send on one channel, via one resolved provider config.

**Properties**

```
Delivery
-----------------
id
execution_id
channel
provider_id
channel_config_id      # which config was actually used (default or tenant-owned)
attempt
status                   # PENDING | SENT | DELIVERED | FAILED_TEMPORARY | FAILED_PERMANENT
rendered_content           # snapshot of what was actually sent (audit trail)
```

**Workflow at this level**

```
Delivery created (PENDING)
      ↓
worker picks up → adapter.send(recipient, rendered_content, channel_config)
      ↓
provider responds:
   accepted → SENT → (webhook later) → DELIVERED
   temp failure → FAILED_TEMPORARY → retry_policy checked → retry or fallback
   permanent failure → FAILED_PERMANENT → Execution moves to next step
```

---

### 1.10 Event

**Purpose:** universal signal that can resume any Execution waiting for it. Channel-agnostic by design.

**Properties**

```
Event
-----------------
id
tenant_id
notification_id        # nullable if not tied to one specific execution
type                     # "otp.verified", "payment.completed", "email.clicked"...
data
received_at
```

**Workflow at this level**

```
POST /events { type, notification_id, data }
      ↓
lookup: Execution WHERE status=WAITING
                    AND notification_id = ?
                    AND waiting_for = type
      ↓ found
atomically transition WAITING → RESUMED (guards against race with timeout sweep)
      ↓
Execution advances to on_event step (back to 1.8 loop)
```

---

**Note:** Broadcasting sits on top of this entire stack (it fans out into many Notifications, each running the 1.7–1.10 loop independently) but is deliberately left out of this section — to be detailed separately.

---

## 1.11 Decisions Log (Phases 1-4 implemented; Phase 5 gate decided)

Every material choice made while building Phases 1-4, plus the Phase 5 gate
outcome. Kept here so the plan and the codebase never drift apart.

### Tooling / infra (recorded per phase below)

- **Data layer:** pgx + sqlc (typed queries generated from SQL; migrations are
  the schema source of truth — sqlc reads golang-migrate files directly).
- **Local dev:** Docker Compose — Postgres 16 on host port **5433**,
  Redis 7 on host port **6380** (default ports 5432/6379 are commonly taken
  by local installs; CI service containers match these host ports).
- **Migrations:** golang-migrate, embedded, auto-applied at API startup;
  `make sqlc` regenerates typed code via the sqlc Docker image.
- **API keys:** HMAC-SHA256 digest under a server pepper (stdlib
  `crypto/hmac`); raw key returned exactly once. Tenant creation returns a
  **bootstrap admin key** (solves the "no key to get a key" chicken-and-egg).
- **Secrets/PII at rest:** AES-256-GCM envelope encryption under a single
  `CREDENTIALS_MASTER_KEY` (hex 32 bytes) — `ChannelConfig.credentials` and
  `ChannelIdentity.value`; identity lookups/uniqueness via a non-reversible
  HMAC **blind index**.
- **Rate limiting:** token buckets in the Channel Resolver keyed by
  `channel_config_id` (`golang.org/x/time/rate`), in-process — fine for one
  replica; multi-replica needs a Redis-backed bucket store (Phase 12).
- **Send queue:** **Asynq (Redis)**, in-process worker inside the API binary
  (plan's "goroutine pool in the same binary" option; swap to SQS later if
  needed). Deliveries are claimed with `SELECT ... FOR UPDATE` so an
  at-least-once queue can never double-send.

### Hardening pass (pre-Phase-12, error-handling/stability sweep)

- **HTTP server timeouts** — `ReadHeaderTimeout` 5s / `Read` 15s / `Write` 30s /
  `Idle` 60s; slow or idle clients can't hold connections open.
- **Bounded request bodies** — router-wide 1 MiB `MaxBytesReader`; a shared
  `decodeJSON` helper maps oversized bodies to **413** (replaces 15
  duplicated `json.NewDecoder` sites with identical 400 handling).
- **Per-request deadline** — 20s context timeout on every route so a hung
  dependency fails fast instead of stalling a connection forever.
- **`/healthz` is dependency-aware** — pings Postgres (pool), Redis
  (`RedisPinger`), and Temporal (`workflow.HealthPinger` via `CheckHealth`);
  200 only when all are reachable, 503 with a per-service breakdown when any
  is down, unconfigured deps reported as "skipped" (not failing).
- **Config fail-fast** — `config.Load()` now returns an error and
  `NEXXNOTIFY_ENV=production` refuses to boot on the well-known dev
  secrets (a public master key would let anyone decrypt every tenant's
  credentials).
- **No swallowed errors in webhook apply path** — `applyDeliveryUpdate`
  propagates DB write failures (the inbound route returns 500 so the
  provider retries the callback instead of silently losing a DELIVERED
  update); `generateWebhookSecret` returns an error instead of panicking;
  `writeJSON` encodes to a buffer (encode failure → 500, never a truncated
  2xx body).

### Design deviations flagged while building

- **Slug PKs for channel types/providers** (`'email'`, `'twilio'`) instead of
  uuid — `channel_type` is already free text on identities/preferences.
- **Twilio hand-rolled REST** (not `twilio-go`) — same rationale the plan
  gives for Vonage/Africa's Talking; the send path is a single testable call.
- **Identity values encrypted, `Notification.data`/`Delivery.rendered_content`
  not** — content is bounded-retention (redacted after 90 days by the sweep)
  and never returned by the API; revisit field encryption if that changes.
- **Platform defaults seeded in code, not migrations** (channels + templates):
  credentials need the runtime key, and code seed data survives test
  truncation. `SeedPlatformDefaults` / `SeedPlatformTemplates` run at startup.

### Phase 5 decision gate — outcome: **adopt Temporal, self-hosted via Docker**

Decision: use the **Temporal Go SDK** with a self-hosted server
(`temporalio/auto-setup` + its Postgres + UI, via Docker Compose and CI
service containers; frontend on :7233). Rationale per the gate table: durable
timers, retries, crash recovery, and distributed workers are the highest-risk
part of this build, and the NOTIFY/WAIT steps map 1:1 onto
Activities/Signals. Consequences for the remaining phases:

- **Phases 5-7 reshape:** the custom execution-engine loop, scheduler,
  locking, and timeout sweep are replaced by Temporal primitives — workflow
  definitions become registered Go functions (not JSON step configs), WAIT =
  `workflow.Sleep`/Timer + Signal selector, the timeout sweep (Phase 7) is
  native, and the `FOR UPDATE SKIP LOCKED` sweeper is no longer needed for
  executions.
- **Retained from the plan:** everything above the engine — Notification
  creation, idempotency, template pinning, `resolveConfig`, the Delivery
  record + status lifecycle, eligibility — stays exactly as designed; the
  Temporal workflow drives the same Activities over the same Delivery rows.
- **Deliveries per channel** are created upfront at Notification creation for
  every channel a workflow might use (plan §1.7's "every channel the
  workflow might use has a Template"), pinned into `resolved_templates`;
  each Activity attempt claims and records its channel's Delivery row.

### Phase 5 build notes (implemented)

Everything below is live and tested (integration suite runs against real
Temporal in CI). Key choices made while building, beyond the gate itself:

- **Workflow definitions are code, selected by key.** `internal/workflow`
  holds a small catalog (`otp_fallback`) — a `Definition` (channels in
  fallback order + default verification wait) plus the registered Go
  workflow function. Adding a workflow = new registered function + catalog
  entry; `GET /workflows` exposes the catalog to tenants.
- **One workflow function, the OTP fallback chain.** `OTPFallbackWorkflow`
  loops over the channel chain: `SendAttemptActivity` per attempt (bounded
  by the Delivery's `max_attempts`, outcome returned as result not error),
  then `workflow.Sleep` + a signal-selector wait for verification
  (Phase 6 wires `POST /events` to the signal). Exhausting the chain without
  verification → `EXPIRED` if anything was ever sent, `FAILED` if nothing
  was. Notification `status` is the workflow's finalize activity's output.
- **Workflow ID = notification ID** (`notif-<id>`): duplicate starts fail
  with `WorkflowExecutionAlreadyStarted`, matching the Phase 4 idempotency
  contract; workflow↔notification linkage is trivial.
- **Migration 000006**: `notifications.workflow` (text key) +
  `workflow_options` (jsonb) columns; `EXPIRED` added to the status CHECK
  (distinct from `FAILED`: sent-but-never-verified vs never-sent).
- **`POST /notifications` with a `workflow` key** (plan §1.7): resolves +
  pins templates for *every* channel the workflow may use, creates one
  PENDING Delivery per channel, commits, then starts the Temporal execution
  (start failure → notification + deliveries marked FAILED, no zombies).
- **Activities as plain functions** (`SendAttemptActivity`, `FinalizeActivity`)
  dispatching through a runtime holder — a method receiver deserializes
  wrongly in the Temporal testsuite mock matcher.
- **Rate-limit exhaustion is not an attempt** (bug found via a CI flake):
  `rate_limited` outcome re-queues without bumping the delivery's attempt
  counter (provider was never called); the workflow bounds it with a 5-streak
  cap. Asynq's retry backstop raised to 20 since rate-limit retries now
  consume queue retries without consuming delivery attempts.
- **Temporal infra in compose + CI**: `temporalio/auto-setup` + its Postgres
  + UI; frontend on `:7233`; `TEMPORAL_URL` config; `go.temporal.io/sdk`
  (v1.47 — `ExecuteWorkflow`, context-level retry policy).

---

## 2. Build Phases (in order)

### Phase 1 — Foundations

- [x] Tenant model + CRUD API (`POST /tenants`, API key issuance, scoping/auth middleware)
- [x] **Recipient / ChannelIdentity / ChannelPreference / Consent model** (1.3) — not a stray field on Notification, a first-class set of tables from day one, since channel eligibility and opt-out depend on it later
- [x] Base data layer (Postgres recommended: relational integrity for tenant/notification/execution links)
- [x] Multi-tenant auth (API key → tenant_id resolution on every request)

**Exit criteria:** a tenant can be created, authenticated, and manage recipients with verified/opted-in channel identities.

---

### Phase 2 — Channel Plugin Architecture

This is the piece that makes "add as many channels as I want" possible. Build it early so nothing later depends on hardcoded channels.

- [x] Define a `ChannelAdapter` interface, e.g.:

    ```
    ChannelAdapter  .validate(recipient, config) -> bool  .send(recipient, payload, config) -> DeliveryResult  .parseWebhook(rawPayload) -> Event | DeliveryStatusUpdate
    ```

- [x] Channel registry — tenants enable channels by name; engine looks up adapter by string, never imports channel-specific code directly.
- [x] Build the first 2–3 adapters to prove the interface: **Push (FCM)**, **Email (SMTP/SES)**, **SMS (Twilio-style)**.
- [x] `ChannelConfig` storage per tenant (encrypted credentials), including `rate_limit` per config.
- [x] `resolveConfig()` — the Channel Resolver — implementing ordered candidate selection, per-config rate-limit checks (token/leaky bucket keyed by `channel_config_id`), and failover-on-exhaustion, per 1.4. Built here, not deferred to a later "add rate limiting" phase — it's part of what resolving a channel means.
- [ ] Provider webhook ingestion → normalizes into internal `DeliveryStatusUpdate` or `Event`. (deferred to Phase 10 — the webhook ingestion surface is already stubbed on the adapters)

**Exit criteria:** a tenant can enable/configure a channel, and you can call `send()` on any registered channel without the core knowing its internals. Adding channel #4 (WhatsApp, Slack, webhook, in-app) requires zero engine changes.

---

### Phase 2.5 — Data Protection & Retention (before going live)

By this point the system already stores real PII — `ChannelIdentity` values (emails, phone numbers, push tokens), `ChannelConfig` credentials, and eventually `Delivery.rendered_content` (which contains actual message bodies, e.g. OTP codes, potentially names/addresses in templated content). This needs a deliberate pass **before** any production traffic, not folded silently into Phase 11/12 hardening later.

- [x] **Data classification** — mark which fields are PII at the schema level (`ChannelIdentity.value`, `Recipient.display_name`, `Notification.data`, `Delivery.rendered_content`) so retention/export/deletion tooling can target them without a manual audit later.
- [x] **Encryption at rest** for PII fields specifically, not just `ChannelConfig.credentials` (which was already called out in 1.4) — extend the same encryption approach to recipient identities and rendered delivery content.
- [x] **Retention policy per data type**, tenant-configurable where reasonable:

    ```
    Delivery.rendered_content  → default retain 30-90 days, then purge or redact                              (it's an audit trail, not meant to be permanent storage                               of every OTP/message ever sent)Event payloads              → similar bounded retentionRecipient/ChannelIdentity   → retained until deletion requested or recipient inactive                               for tenant-configured periodExecution/Notification metadata (status, timestamps) → can outlive the content itself
    ```

- [x] **Right-to-be-forgotten / deletion API** — `DELETE /recipients/:id` that cascades: purges `ChannelIdentity`, redacts `rendered_content` on past `Delivery` rows tied to that recipient, and definesa clear stance on whether aggregate `Broadcast.stats` counters are adjusted retroactively (they generally should not be — anonymize the row, don't rewrite historical aggregates).
- [x] **Consent audit trail** already exists structurally (1.3's `Consent` table) — confirm it's populated correctly before launch, since it's the evidence trail for opt-in/opt-out compliance, not optional bookkeeping.
- [ ] **Data residency**, if relevant to target markets — where `ChannelConfig` credentials and recipient PII are physically stored, especially once tenants bring their own provider accounts across regions. *(deployment-time decision, noted in README)*

**Exit criteria:** a clear, enforced answer to "where does this piece of PII live, how long does it live there, and how does it get deleted" for every entity that touches recipient data — before Phase 4 starts generating real Deliveries with real content.

---

### Phase 3 — Templates

- [x] Template model: `(tenant_id, type, channel, body, variables)`
- [x] Rendering engine (variable substitution, per-locale optional)
- [x] Template validation per channel (e.g. SMS length limits, email HTML)

---

### Phase 4 — Simple Notification (no workflow yet)

Get a linear path working before adding the state machine.

- [x] `POST /notifications` → creates Notification (`status: PENDING`)
- [x] **Idempotency key support** (`Idempotency-Key` header, unique constraint on `(tenant_id, recipient_id, idempotency_key)`, atomic lookup-or-create) — built here, not deferred, because a network-timeout retry from any caller must never double-send. Not a "hardening" feature; it's part of what `POST /notifications` means.
- [x] Direct-send mode: notification specifies a single channel, no workflow → immediate Delivery
- [x] Delivery model + worker/queue (send job picked up async)
- [x] Delivery status lifecycle: `PENDING → SENT → DELIVERED / FAILED`
- [x] Basic retry-on-failure (fixed attempts, no workflow logic yet)

**Exit criteria:** any service can `POST /notifications` and get a push/email/sms sent and tracked exactly once per idempotency key, with no waiting/branching.

---

### Decision Gate — before Phase 5

Stop here and decide **before** writing any execution-engine code: this phase is effectively building a durable workflow engine (durable timers, retries, crash recovery, distributed workers) _inside_ the notification product. That's a legitimate but nontrivial undertaking — worth a deliberate choice rather than defaulting into it.

||Build it yourself|Adopt Temporal (or similar)|
|---|---|---|
|Best when|you want full control over the execution model; workflows stay relatively simple (the NOTIFY/WAIT/CALL_API/CONDITION shape in this doc)|you need durable timers, automatic retries, crash recovery, and distributed workers without hand-rolling all of it|
|Cost|you own Phases 5–7 end to end (scheduler, locking, race handling, versioning)|integration + learning curve, but Phases 5–7's hardest parts (7 especially) come largely for free|
|Risk|correctness bugs in your own scheduler/locking are the most likely source of subtle production incidents in this whole system|dependency on an external system's operational model|

**Decision (recorded in §1.11): adopt Temporal**, self-hosted via Docker
(`temporalio/auto-setup` + its Postgres + UI, Go SDK). Phases 5-7 below are
scoped for that choice — Temporal owns the execution engine, durable timers,
retries, crash recovery, and the timeout sweep. The custom scheduler/locking
work in the original Phase 7 is no longer needed.

---

### Phase 5 — Workflow Engine (the core differentiator, on Temporal)

- [x] Workflow definitions as registered Go functions (Temporal SDK) — the plan's NOTIFY/WAIT/CONDITION step shapes map onto Activities/Selectors; a small built-in catalog (`otp_fallback`, ...) is selected by key. *(Replaces the JSON/YAML step schema — with Temporal, step logic is code, not config; tenant-custom workflows = new registered workflow functions.)*
- [x] `GET /workflows` catalog API per tenant — discoverable workflow keys + the channels each one may use
- [x] Execution model — **owned by Temporal**: durable timers, retries, crash recovery, history. Notification keeps `status` for the API surface; the Delivery rows stay the per-attempt record
- [x] Execution engine loop — Temporal provides it ("if WAITING: exit" = `workflow.Sleep`/Timer + Signal selector; no blocking worker)
- [x] Channel resolution by **goal + preferred channel list** (not hardcoded channel per notification) — reuses Phase 2 registry + real-time eligibility checks (opted-out, provider health, valid identity)
- [x] `POST /notifications` with a `workflow` key: resolve + pin templates for **every channel the workflow might use** (plan §1.7), create one PENDING Delivery per channel, start a Temporal execution (workflow ID = notification id)

**Exit criteria:** the OTP example (push → wait → timeout → email → wait → timeout → sms → expire) runs end to end — with Temporal, the waits are durable timers and the fallback chain is the workflow's own loop over Activities.

---

### Phase 6 — Events & Resumption (signals)

- [x] `POST /events` public API (`{type, notification_id, data}`) → `client.SignalWorkflow(workflowID, signalName, payload)`
- [x] Event → workflow matching: the workflow's Signal selector (registered in Phase 5) resumes the right wait; no index/lookup table needed
- [x] Resume logic: signal received → advance to the on_event path; timeout still wins if it fires first (Temporal selector semantics — exactly one path wins)
- [x] Idempotency: dedupe repeated events — an `events` table (plan §1.10) with a fingerprint unique constraint + already-resumed check; replayed events return the recorded row (200) without re-signaling
- [x] Action-link support (email/SMS buttons that hit an action endpoint → translate click into an Event)

**Design note (recorded here):** Temporal does the *matching* (signal name =
wait selector) — the `events` table is not a lookup index. It exists for
**idempotency** (a caller retry after a network timeout must not double-signal)
and **audit/retention** (which events arrived, when — bounded retention like
the rest of the PII content, plan §1.10), and the retention sweep covers it.

### Phase 6 build notes (implemented)

- **Migration 000007 — `events` table** (plan §1.10): tenant-scoped, FK to
  the notification, with a `UNIQUE (fingerprint)` constraint where the
  fingerprint is sha256 over `tenant|notification|type|data` (as-received).
  `events.data` is `pii:content` — the 90-day retention sweep redacts it
  (the sweep's `events.data` statement was pre-registered; the table now
  exists so it applies).
- **`POST /events` validates against the workflow catalog**: the event type
  must be one the notification's workflow declares it waits on
  (`Definition.Events`, e.g. `otp_fallback` → `["otp.verified"]`) — a type
  nothing is waiting for fails fast with 400 instead of silently no-op'ing.
  Direct-send notifications (no workflow) are rejected with 400.
- **Signal-first, then record.** If the DB write fails after a successful
  signal, a caller retry re-signals (harmless — the workflow already
  resumed) and records; recording first would dedupe the retry on the
  fingerprint and leave the workflow stuck. A signal to an already-finished
  workflow (timeout won / already verified) surfaces as
  `serviceerror.NotFound` → the event is still recorded for audit and the
  API reports success (200/201).
- **Action link — `GET /actions/verify?notification_id=&code=`**: the code
  rendered into the message is the proof (constant-time compare against
  `notification.data.code`), so the endpoint is intentionally
  unauthenticated; a correct click records + delivers an `otp.verified`
  event via the same path as `POST /events` (idempotent — replaying the
  link returns the recorded event). Wrong code and unknown notification are
  indistinguishable (404).
- **Action link wired into the platform email template (demo).** The
  platform OTP email body renders `Verify instantly: {{verify_url}}`.
  `verify_url` is injected by `POST /notifications` at render time from
  `ACTION_BASE_URL` + the notification id + `data.code` — the handler
  pre-generates the notification UUID (rendering happens before the row
  exists; `CreateNotificationIfAbsent` now takes the explicit id). The
  injected variable is derived, never stored in `notification.data`.
  Platform seeding now *converges* existing PUBLISHED defaults whose body
  drifted from the code seed (code is the source of truth; pinned
  notifications are unaffected since content is snapshotted at creation).
- **Action links are expiry-aware.** The workflow already encodes "the OTP
  window lapsed" as notification status `EXPIRED` (sent, never verified)
  vs `FAILED` (nothing ever sent) — the link handler rejects a click with
  the correct code on either with `410 Gone` and records no event. The
  code check runs first, so a wrong code is always `404` (probes can't
  distinguish an existing notification); `COMPLETED` replays idempotently
  (200, same recorded event).
- **`events:write` scope** added; the audit trail is exposed via
  `GET /notifications/{id}/events` (`notifications:read`, oldest first,
  tenant-scoped — no existence leak).

---

*(The pre-Temporal "Events & Resumption" section above was superseded by the
Temporal-flavored version — with Temporal, event→wait matching is the
workflow's Signal selector, and the custom `Execution`/`WAITING`/indexed-lookup
machinery never existed in code. Kept only the (signals) version.)*

---

### Phase 7 — Timeout Scheduling (native to Temporal)

- [x] Durable timers — `workflow.Sleep`/`workflow.NewTimer` in the workflow; no cron sweeper, no `FOR UPDATE SKIP LOCKED` claiming (Temporal owns it)
- [x] Locking/claim strategy across replicas — Temporal's task processing (multiple workers share the task queue safely)
- [x] Race handling (event vs timeout at the same instant) — single selector fires exactly one branch
- [x] Resume to timeout path — the selector's timer callback

**Exit criteria:** waits reliably resolve via event OR timeout, never both, never neither — guaranteed by Temporal's selector semantics.

---

### Phase 8 — Retries & Fallback Chains

- [x] Delivery-level retry policy (attempts, backoff) per channel — bounded retry loop inside the workflow (reuses the Delivery `max_attempts` ceiling) and/or Temporal activity `RetryPolicy`
- [x] Fallback chain: exhausted retries on channel A → auto-advance to channel B — the workflow's channel loop (built in Phase 5)
- [x] Provider-health checks feeding into channel eligibility (Phase 2 registry) — circuit breaker ahead of the adapter

**Phase 8 build notes (implemented)**

- **Per-config circuit breaker lives in the Channel Resolver**, mirroring the
  token-bucket `Buckets` pattern: a `Breakers` registry keyed by
  `channel_config_id`, closed → open → half-open. 5 consecutive
  **temporary** failures trip it (one busy retry chain, not a blip); while
  open, `resolveConfig` treats the candidate as ineligible and fails over to
  the next enabled config (same path as rate-limit exhaustion); after 60s a
  single half-open probe is admitted, and a successful probe closes it — a
  sick provider recovers without any manual intervention.
- **Only temporary failures feed the breaker** — a permanent failure is a
  recipient/config problem, not provider health; success closes/resets.
  Reported from the single send path (`AttemptService.PerformAttempt`), so
  both the direct-send worker and the Temporal activity get it. Hand-rolled
  in-process registry (same Phase 12 multi-replica note as the buckets);
  `sony/gobreaker` considered but not adopted — the state machine is ~70
  lines and the project convention is dependency-light registries.

---

### Phase 9 — Broadcasting

- [ ] Audience model (static list, segment/query, or upload)
- [ ] `POST /broadcasts` → `{audience, notification_template, workflow, schedule, rate_limit}`
- [ ] Fan-out worker: for each audience member → create an independent Notification + Execution (reuses everything from Phases 4–8, no new logic)
- [ ] Rate limiting / throttled dispatch (protect provider quotas at 2M-recipient scale)
- [ ] Broadcast status aggregation (roll-up of individual notification statuses; broadcast itself stays "RUNNING" independent of per-user outcomes)

**Exit criteria:** a broadcast to N users produces N independent executions, each capable of its own wait/fallback/timeout path, without a single blocking call.

**Do not** insert millions of Notification rows in one transaction. At scale this phase additionally needs: audience snapshots (immutable, streamed — not loaded into memory), batch/paginated fan-out, per-tenant provider quota awareness (ties to Channel Config, 1.4), pause/resume/cancel controls, progress aggregation via counters (not table scans), and duplicate-prevention across fan-out retries. Full detail intentionally deferred — see note at the end of Section 1.

---

### Phase 10 — External Service Integration

- [x] Outbound API-call step type in workflows (`CALL API` — e.g. notify Platform X's backend on approval)
- [x] Auth/secrets management for outbound calls per tenant
- [x] Retry/failure handling for outbound calls (same retry primitives as Phase 8)
- [x] Inbound webhook normalization for third-party services (payment provider, CRM, etc.) → Events (reuses Phase 6)

**Phase 10 build notes (implemented)**

- **Migration 000008** — three tables: `webhook_endpoints` (tenant
  outbound CALL API config: url/method + optional HMAC secret,
  envelope-encrypted), `webhook_deliveries` (per-attempt callback audit:
  status/http_status/error, one row per activity execution), and
  `inbound_webhooks` (signed ingress bridge: the endpoint id IS the URL
  credential, the secret is generated, returned once, encrypted).
- **CALL API step = `CallWebhookActivity`** on the workflow: `POST
  /notifications` accepts `callback_endpoint_id` (tenant-owned, validated
  404 on unknown/cross-tenant, 400 without a workflow) and the
  `otp_fallback` workflow fires a signed POST to it **after** finalizing
  COMPLETED — the plan's "notify the backend on approval". Best-effort by
  design: the notification is already terminal, so a failed callback must
  not roll it back; failures are retried and audited. Payload =
  notification_id + status + notification_type + completed_at, signed
  `X-NexxNotify-Signature: sha256=<HMAC>` when the endpoint has a secret.
- **Retry/failure = same primitives as Phase 8.** The activity's
  `RetryPolicy` is bounded exponential (1s→2s→4s, 3 attempts) and the
  activity classifies: 2xx → SUCCEEDED, 4xx/permanent (bad URL, secret
  undecryptable, cross-tenant, disabled) → NonRetryableApplicationError,
  5xx/transport → retryable. Every attempt records a `webhook_deliveries`
  row; `GET /notifications/{id}/webhook-deliveries` exposes the audit trail.
- **Inbound = signed generic event envelope** at `POST /webhooks/{endpointID}`
  (unauthenticated — the id + HMAC signature are the auth): a third-party
  service (payment provider, CRM, ...) POSTs
  `{type, notification_id, data}`, the handler verifies the signature
  (constant-time), validates the notification is in the endpoint's tenant
  and workflow-driven, then feeds the **exact same Phase 6 path**
  (`recordAndSignalEvent`) — record for idempotency/audit + signal the
  waiting workflow. 401 on bad signature, 404 on unknown endpoint, replay
  idempotent (200).
- **Provider-bound webhooks (real Twilio status callbacks):** an inbound
  webhook can be bound to a provider adapter (`provider: "twilio"`), with
  the tenant's **Twilio auth token** as the secret (required at creation;
  validated against the adapter registry). The route then verifies
  `X-Twilio-Signature` using Twilio's documented algorithm (base64
  HMAC-SHA1 over the full URL + sorted URL-decoded body params, constant
  time), calls `TwilioAdapter.ParseWebhook` — rewritten to parse the real
  **form-encoded** wire format (JSON accepted as a fallback; SmsSid alias;
  ErrorMessage carried through; a signed non-status payload is a no-op, not
  an error) — and applies the normalized `DeliveryStatusUpdate` to the
  matching delivery **scoped by provider message id + tenant**
  (`delivered` → `DELIVERED`, `failed`/`undelivered` →
  `FAILED_PERMANENT` with the carrier's error). Signed-but-unknown SIDs are
  acked and ignored (200); a replay is idempotent; a bad signature is 401.
  Migration 000009 adds the `provider` column; the Phase 2 `ParseWebhook`
  stub is now a real adapter contract with a tested implementation.
- **CRUD + scopes**: `webhook-endpoints` and `inbound-webhooks`
  collections behind new `webhooks:read`/`webhooks:write` scopes; inbound
  secret returned exactly once. `sony/gobreaker`-style deps avoided —
  hand-rolled HMAC + standard-library signing, matching the project's
  dependency-light convention.

---

### Phase 11 — Observability & Ops

- [ ] Execution history / audit trail per notification (already implied by state transitions — expose via API)
- [ ] Dashboards: delivery success rate per channel/tenant, broadcast progress, stuck/waiting executions, **rate-limit hit frequency per `channel_config_id`** (visibility into a mechanism built in Phase 2, not building the mechanism itself here)
- [ ] Dead-letter handling for permanently failed executions
- [ ] Workflow versioning: running executions pin to the workflow version they started with; new workflow edits only apply to new executions
- [ ] Alerting on provider outages / spike in `FAILED_TEMPORARY`

---

### Phase 12 — Hardening

- [ ] Idempotency keys on `POST /broadcasts` specifically (per-notification idempotency already landed in Phase 4 — broadcasts need their own key so re-triggering the same broadcast doesn't re-fan-out)
- [ ] Multi-region/HA scheduler (avoid single point of failure for timeout sweeping)
- [ ] Load testing broadcast fan-out at target scale
- [ ] Tenant-level sandbox/test mode (send without hitting real providers)
- [ ] Tenant-level quotas (daily/monthly send caps per channel, separate from provider rate limits)

---

## 3. API Surface (target shape)

```
Tenants
  POST   /tenants
  POST   /tenants/:id/channels           # enable + configure a channel
  POST   /tenants/:id/templates
  POST   /tenants/:id/workflows

Notifications
  POST   /notifications                  # create + trigger
  GET    /notifications/:id              # status + execution history

Events
  POST   /events                         # resume waiting executions

Broadcasts
  POST   /broadcasts
  GET    /broadcasts/:id                 # aggregate status

Webhooks (per channel, provider-specific, normalized internally)
  POST   /webhooks/:channel
```

---

## 4. Tools Per Phase (Go)

Concrete Go-ecosystem tool choices mapped to each build phase. Pick one option per row.

### Phase 1 — Foundations (Tenant, Auth, Recipient)

|Need|Options|
|---|---|
|API framework|**chi** (thin, stdlib-compatible, most idiomatic for this kind of service) · **Echo** or **Gin** if you want more batteries-included routing/middleware|
|Primary DB|**PostgreSQL** — relational integrity matters everywhere (tenant → notification → execution → delivery FK chains)|
|DB driver/query layer|**pgx** (driver) + **sqlc** (generates typed Go from SQL, keeps queries explicit rather than hiding them behind an ORM) — or **GORM** if the team prefers an ORM|
|API key hashing|**crypto/hmac** + **crypto/sha256** (stdlib) with a server-side pepper — fast, correct choice for per-request auth; avoid `bcrypt` here, it's intentionally slow and meant for passwords, not high-frequency key checks|
|Auth middleware|plain `chi` middleware func (hash key → lookup → attach `tenant_id` to `context.Context`) — no library needed|
|Migrations|**golang-migrate** or **goose**|

### Phase 2 — Channel Plugin Architecture

|Need|Options|
|---|---|
|Adapter interface|plain Go interface (`type ChannelAdapter interface { Validate(...); Send(...); ParseWebhook(...) }`) — your own abstraction, no library|
|Push (FCM)|`firebase.google.com/go/v4/messaging` (official Firebase Admin SDK for Go)|
|Email (SMTP/SES)|stdlib `net/smtp` or `gomail` for SMTP · `aws-sdk-go-v2/service/ses` for SES|
|Email (SendGrid)|`sendgrid-go`|
|SMS (Twilio)|`twilio-go` (official)|
|SMS (Vonage)|REST via stdlib `net/http` — Vonage's Go SDK is community-maintained and less mature; calling their HTTP API directly is often more reliable|
|SMS (Africa's Talking)|no official Go SDK — REST via `net/http`, same pattern as Vonage|
|Secrets/credential encryption|**AWS KMS Go SDK** (`aws-sdk-go-v2/service/kms`) or **HashiCorp Vault** (`hashicorp/vault/api`) for envelope-encrypting `ChannelConfig.credentials`|
|Rate limiting (per-config, in the Resolver)|`golang.org/x/time/rate` if single-instance is enough; for distributed (multi-replica) limits keyed by `channel_config_id`, use Redis + `ulule/limiter` (has a Redis store) or hand-roll a token bucket with `go-redis` + `INCR`/`EXPIRE`|

### Phase 2.5 — Data Protection & Retention

|Need|Options|
|---|---|
|Field-level encryption (PII)|**AWS KMS** / **Vault Transit** for envelope encryption · Postgres **pgcrypto** if you want column-level encryption done DB-side|
|Retention/purge jobs|scheduled job via `robfig/cron` (see Phase 7) running `DELETE`/redaction queries on a schedule|
|Data classification tracking|schema annotations/documentation — process, not infrastructure|

### Phase 3 — Templates

|Need|Options|
|---|---|
|Variable substitution|stdlib `text/template` (logic-capable but keep it disciplined) or `cbroglie/mustache` if you want to enforce the "no logic in templates" rule at the tooling level|
|Email HTML (if building the block-based/MJML route later)|Go has **no native MJML compiler** — call the hosted **MJML API** over HTTP, or shell out to the Node-based `mjml` CLI as a sidecar process if you want it self-hosted. This is the one spot in the stack where you can't stay pure-Go without extra work.|
|Plain-text email fallback generation|`jaytaylor/html2text`|

### Phase 4 — Simple Notification + Idempotency

|Need|Options|
|---|---|
|Queue (decouple create from send)|**Asynq** (Redis-backed task queue, purpose-built for Go — closest equivalent to BullMQ) · **AWS SQS** (`aws-sdk-go-v2/service/sqs`) · **NATS JetStream** if you want pub/sub + queue semantics together|
|Idempotency constraint|native Postgres **unique constraint** on `(tenant_id, recipient_id, idempotency_key)` — DB-level guarantee, no library|
|Worker process|separate Go binary (or goroutine pool in the same binary for small scale) consuming the queue|

### Decision Gate — before Phase 5

|Option|Tool|
|---|---|
|Build it yourself|Postgres for execution state + `robfig/cron` or Asynq's scheduled tasks for the sweep (see Phase 7)|
|Adopt a durable engine|**Temporal** — has a first-class **Go SDK**, and Workflows/Activities map directly onto your NOTIFY/WAIT/CALL_API/CONDITION steps|

### Phase 5 — Workflow Engine

|Need|Options|
|---|---|
|Workflow definition format|JSON validated with `invopop/jsonschema` / `xeipuuv/gojsonschema`, or YAML via `gopkg.in/yaml.v3`|
|Execution engine (if self-built)|plain Go application code — the "load → evaluate → persist" loop doesn't need a framework|
|Execution engine (if Temporal)|Temporal Go SDK — Workflows and Activities map directly onto your step types|

### Phase 6 — Events & Resumption (Temporal)

|Need|Options|
|---|---|
|Event ingestion API|same `chi`/Echo setup as Phase 1|
|Event → workflow matching|Temporal `client.SignalWorkflow` + the workflow's Signal selector — no Postgres lookup|
|Idempotent event dedupe|unique constraint on an event fingerprint column (same pattern as Phase 4) — insert-or-ignore, replay returns the recorded row|
|Signal delivery|Temporal client's `SignalWorkflow` (idempotent at-most-once per run); the events table makes the *API* idempotent|

### Phase 7 — Timeout Scheduling

|Need|Options (if self-built, i.e. not using Temporal)|
|---|---|
|Sweep job scheduling|`robfig/cron` in-process, or Asynq's periodic task support, or a managed scheduler (**AWS EventBridge Scheduler**) triggering a worker|
|Concurrency-safe claiming|Postgres `SELECT ... FOR UPDATE SKIP LOCKED` — built into Postgres, no library|

### Phase 8 — Retries & Fallback Chains

|Need|Options|
|---|---|
|Backoff logic|`cenkalti/backoff` (widely used, supports exponential + jitter)|
|Provider health checks|`sony/gobreaker` for circuit-breaking a flaky provider before it burns through retries|

### Phase 9 — Broadcasting (not detailed yet, tools only)

|Need|Options|
|---|---|
|Audience file ingestion|stdlib `encoding/csv`, streamed row-by-row — never load the whole file into memory|
|Fan-out queue|same queue as Phase 4 (**Asynq**/**SQS**), publishing at higher volume with pacing|
|Blob storage for uploaded audience files|**S3** via `aws-sdk-go-v2/service/s3`|

### Phase 10 — External Service Integration

|Need|Options|
|---|---|
|Outbound HTTP calls (CALL_API step)|stdlib `net/http` wrapped with `hashicorp/go-retryablehttp` for retry/backoff|
|Inbound webhook signature verification|stdlib `crypto/hmac` following each provider's documented signing scheme (Twilio, Stripe-style, etc.)|

### Phase 11 — Observability & Ops

|Need|Options|
|---|---|
|Dashboards|**Grafana** over **Prometheus** metrics exported via `prometheus/client_golang`|
|Structured logging|`rs/zerolog` or `uber-go/zap`|
|Tracing|**OpenTelemetry Go SDK**, exported to **Jaeger** / **Honeycomb** / Datadog APM|
|Alerting|Grafana Alerting, PagerDuty, or Datadog Monitors, wired to `FAILED_TEMPORARY` spike / rate-limit-hit metrics|

### Phase 12 — Hardening

|Need|Options|
|---|---|
|Multi-region/HA scheduler|if on Temporal: built-in · if self-built: multiple scheduler replicas behind the existing `FOR UPDATE SKIP LOCKED` claim pattern|
|Load testing broadcast fan-out|**vegeta** (Go-native HTTP load testing tool, fits naturally in an all-Go toolchain) or **k6**|
|Sandbox/test mode|a `Tenant.mode = "test"` flag that routes `resolveConfig()` to no-op/mock adapters — no library, just a resolver branch|

---

## 5. Build Order Summary (short version)

```
Tenant + Auth
   ↓
Channel Plugin Interface (+ 2-3 adapters, rate-limit-aware resolver)
   ↓
Data protection & retention pass (before real PII flows)
   ↓
Templates
   ↓
Simple linear Notification send (idempotent from day one)
   ↓
Decide: build workflow engine yourself, or adopt Temporal
   ↓
Workflow engine (steps, conditions)
   ↓
Wait/Event/Timeout resumption
   ↓
Retry + fallback chains
   ↓
Broadcasting (fan-out)
   ↓
External service integration (outbound calls + inbound webhooks)
   ↓
Observability, versioning, hardening
```

Each phase is independently testable and builds only on what came before — you can ship Phase 4 as a usable product before the workflow engine even exists.