#!/usr/bin/env bash
#
# deploy.sh — Deploy nexxnotify to a server with Docker Compose.
#
# Usage (run on the server, inside this repo):
#   ./deploy.sh                    # build + start (default: up)
#   ./deploy.sh up                 # build & start the stack
#   ./deploy.sh restart            # recreate containers with a fresh build
#   ./deploy.sh down               # stop (keep volumes)
#   ./deploy.sh logs [service]     # tail logs (default: api)
#   ./deploy.sh status             # docker compose ps + health check
#
# Before first deploy: copy your .env to the server (same directory as this
# script) or run with no .env to bootstrap one interactively.
#
# Env vars:
#   ENV_FILE    path of the env file to use (default: .env)
#
set -euo pipefail

ENV_FILE="${ENV_FILE:-.env}"
APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$APP_DIR"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
ok()    { printf '\033[1;32m✔\033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m!\033[0m %s\n' "$*" >&2; }
fail()  { printf '\033[1;31m✖\033[0m %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing dependency: $1"
}

COMPOSE=()
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  fail "docker compose (or docker-compose) is required"
fi

compose() { "${COMPOSE[@]}" "$@"; }

# ---------------------------------------------------------------------------
# Env bootstrap
# ---------------------------------------------------------------------------

bootstrap_env() {
  info "No $ENV_FILE found — bootstrapping from .env.example"
  cp .env.example "$ENV_FILE"

  default_host="smtp.gmail.com"
  read -rp "SMTP host [$default_host]: " smtp_host
  smtp_host="${smtp_host:-$default_host}"

  read -rp "SMTP username (e.g. you@gmail.com): " smtp_user
  [ -n "$smtp_user" ] || fail "SMTP username is required"

  read -rsp "SMTP password (e.g. Gmail app password): " smtp_pass
  printf '\n'
  [ -n "$smtp_pass" ] || fail "SMTP password is required"

  read -rp "From address [$smtp_user]: " smtp_from
  smtp_from="${smtp_from:-$smtp_user}"

  sed -i.bak \
    -e "s|^SMTP_HOST=.*|SMTP_HOST=$smtp_host|" \
    -e "s|^SMTP_USERNAME=.*|SMTP_USERNAME=$smtp_user|" \
    -e "s|^SMTP_PASSWORD=.*|SMTP_PASSWORD=$smtp_pass|" \
    -e "s|^SMTP_FROM=.*|SMTP_FROM=$smtp_from|" \
    "$ENV_FILE"
  rm -f "$ENV_FILE.bak"
}

ensure_env() {
  [ -f "$ENV_FILE" ] || bootstrap_env
  chmod 600 "$ENV_FILE"

  # Required for email sending
  for key in SMTP_HOST SMTP_USERNAME SMTP_PASSWORD; do
    value="$(grep -E "^${key}=" "$ENV_FILE" | head -n1 | cut -d= -f2- || true)"
    [ -n "$value" ] || fail "$key is empty in $ENV_FILE — set it (copy your local .env to the server)"
  done

  # Inside the compose network the DB host is "postgres:5432", not "localhost:5433".
  if grep -q '^DATABASE_URL=.*@localhost:[0-9]*' "$ENV_FILE"; then
    info "Rewriting DATABASE_URL in $ENV_FILE for the Docker network (localhost -> postgres:5432)"
    # Keep a one-time backup so the local-dev value can be restored later
    [ -f "$ENV_FILE.local-backup" ] || cp "$ENV_FILE" "$ENV_FILE.local-backup"
    sed -i.bak -E 's#@localhost:[0-9]+#@postgres:5432#' "$ENV_FILE"
    rm -f "$ENV_FILE.bak"
  fi

  if ! grep -q '^DATABASE_URL=' "$ENV_FILE"; then
    fail "DATABASE_URL is missing from $ENV_FILE"
  fi

  ok "Using $ENV_FILE"
}

# ---------------------------------------------------------------------------
# Checks
# ---------------------------------------------------------------------------

wait_for_health() {
  local base_url="${BASE_URL:-http://localhost:8080}"
  local seconds="${1:-120}"

  info "Waiting for API to become healthy at $base_url/healthz (up to ${seconds}s)..."
  local elapsed=0
  while [ "$elapsed" -lt "$seconds" ]; do
    if [ "$(command -v curl)" ] && curl -fsS "$base_url/healthz" >/dev/null 2>&1; then
      ok "API is healthy"
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  warn "API did not report healthy within ${seconds}s — check logs: ${COMPOSE[*]} logs api"
  return 1
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------

cmd_up() {
  require_cmd curl
  ensure_env
  info "Building images..."
  compose build api
  info "Starting services (postgres + api)..."
  compose up -d
  ok "Stack is up:"
  compose ps
  wait_for_health || true
}

cmd_restart() {
  require_cmd curl
  ensure_env
  info "Rebuilding and recreating containers..."
  compose up -d --build --force-recreate
  ok "Containers recreated:"
  compose ps
  wait_for_health || true
}

cmd_down() {
  compose down
  ok "Stack stopped (data volume kept)"
}

cmd_logs() {
  local service="${1:-api}"
  compose logs -f --tail=100 "$service"
}

cmd_status() {
  compose ps
  if [ "$(command -v curl)" ] && curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; then
    ok "API healthy at http://localhost:8080/healthz"
  else
    warn "API not reachable at http://localhost:8080"
  fi
}

cmd_help() {
  sed -n '2,12p' "$0"
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------

case "${1:-up}" in
  up)       cmd_up ;;
  restart)  cmd_restart ;;
  down)     cmd_down ;;
  logs)     cmd_logs "${2:-}" ;;
  status)   cmd_status ;;
  help|-h|--help) cmd_help ;;
  *) fail "unknown command: $1 (use: up | restart | down | logs | status)" ;;
esac