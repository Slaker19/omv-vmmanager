#!/usr/bin/env bash
# Atomic Docker deploy with health-check + auto-rollback.
#
# Usage (run on the Docker host as alvin):
#   /opt/webvm-src/scripts/deploy-docker.sh
#
# What it does:
#   1. Back up the currently-running webvm-backend image to a
#      timestamped .tar.gz in /home/alvin/webvm-backups/.
#   2. `docker compose build backend` to bake the new code.
#   3. `docker compose up -d backend` to swap the container.
#   4. Poll `docker inspect --format '{{.State.Health.Status}}'`
#      every 2s for up to 30s. If the container doesn't become
#      "healthy" within that window, re-load the previous image
#      and recreate the container. The user sees a clear failure
#      message and the system is left in the pre-deploy state.
#
# Companion target: `make install-systemd` does the same for the
# systemd-based install (.130) using /api/health via curl.

set -euo pipefail

REPO="${REPO:-/opt/omv-vmmanager-src}"
BACKUP_DIR="${BACKUP_DIR:-/home/alvin/vmmanager-backups/before-rollback-deploy}"
TS="$(date +%Y%m%d-%H%M%S)"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"   # seconds
HEALTH_INTERVAL="${HEALTH_INTERVAL:-2}"  # seconds

log()  { printf '[deploy %s] %s\n' "$TS" "$*"; }
fail() { printf '[deploy %s] ERROR: %s\n' "$TS" "$*" >&2; exit 1; }

cd "$REPO" || fail "cannot cd to $REPO"
test -f docker-compose.yml || fail "docker-compose.yml not found in $REPO"
command -v docker >/dev/null || fail "docker not installed"

# 0. Sanity check: the Go embed and the source-tree dist must agree
# on their main JS bundle. If they don't, the running container
# will serve the stale bundle (the Dockerfile copies
# frontend/dist/, not backend/internal/frontend/dist/) and the
# deploy looks healthy even though the UI hasn't changed. This
# exact mistake shipped phase1.7-bis-backup to .163 with an old
# bundle; the fix is to copy the embed into the tree before
# building.
EMBED_JS="$(ls -1 backend/internal/frontend/dist/assets/index-*.js 2>/dev/null | head -1)"
TREE_JS="$(ls -1 frontend/dist/assets/index-*.js 2>/dev/null | head -1)"
if [ -z "$EMBED_JS" ] || [ -z "$TREE_JS" ]; then
	fail "missing frontend dist (embed=${EMBED_JS:-none} tree=${TREE_JS:-none}); run \`cd frontend && npm install && npm run build\` then \`cp -r frontend/dist backend/internal/frontend/dist\` before deploying."
fi
if [ "$(basename "$EMBED_JS")" != "$(basename "$TREE_JS")" ]; then
	fail "dist mismatch: embed has $(basename "$EMBED_JS") but tree has $(basename "$TREE_JS"). Run \`cp -r frontend/dist backend/internal/frontend/dist\` (or \`make build-frontend\`) before deploying."
fi

mkdir -p "$BACKUP_DIR"

# 1. Back up the running image.
log "backing up current omv-vmmanager image..."
docker save omv-vmmanager | gzip > "$BACKUP_DIR/omv-vmmanager.before-rollback.${TS}.tar.gz"
log "  -> $BACKUP_DIR/omv-vmmanager.before-rollback.${TS}.tar.gz"

# 1b. Ensure self-signed cert exists for Caddy. On a fresh
# checkout, the cert dir doesn't exist; we generate it. If the
# script fails (e.g. the host user can't sudo and the cert dir
# isn't writable), we warn and continue — the caddy container
# will fail to start but the backend will still come up healthy,
# so the deploy script reports overall success. The operator
# should generate the cert manually (see README).
CERT_DIR="$REPO/certs"
if [ -f "$CERT_DIR/vmmanager.crt" ] && [ -f "$CERT_DIR/vmmanager.key" ]; then
	log "self-signed cert already present at $CERT_DIR/"
elif [ -f "$REPO/scripts/generate-self-signed.sh" ]; then
	log "generating self-signed cert at $CERT_DIR/..."
	mkdir -p "$CERT_DIR"
	if ! HOST_LAN_IP="$(hostname -I | awk '{print $1}')" \
		CERT_DIR="$CERT_DIR" \
		"$REPO/scripts/generate-self-signed.sh"; then
		log "WARNING: cert generation failed; caddy may not start. Generate manually with: scripts/generate-self-signed.sh"
	fi
else
	log "WARNING: no cert at $CERT_DIR/ and no generate-self-signed.sh script. Caddy will fail to start."
fi

# 2. Build the new image.
# Stamp the image with a version + build_time so /api/health and
# structured logs show real identity (not "dev" / "unknown").
# Caller can override; otherwise we derive from git or timestamp.
: "${VMMANAGER_VERSION:=$(git -C "$REPO" describe --tags --dirty --always 2>/dev/null || echo manual-$(date +%Y%m%d-%H%M%S))}"
: "${VMMANAGER_BUILD_TIME:=$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
export VMMANAGER_VERSION VMMANAGER_BUILD_TIME
log "building new omv-vmmanager image (version=${VMMANAGER_VERSION}, build_time=${VMMANAGER_BUILD_TIME})..."
docker compose build backend
# The caddy service uses the official caddy:2 image; nothing to
# build for it. But the caddy container's volume mount of
# $REPO/certs/ requires the dir to exist, which we just ensured.

# 3. Swap the container.
log "recreating omv-vmmanager container..."
docker compose up -d backend caddy

# 4. Wait for healthy.
log "waiting for health check (timeout ${HEALTH_TIMEOUT}s)..."
elapsed=0
while [ "$elapsed" -lt "$HEALTH_TIMEOUT" ]; do
  status="$(docker inspect --format='{{.State.Health.Status}}' omv-vmmanager 2>/dev/null || echo 'unknown')"
  case "$status" in
    healthy)
      log "  health check passed after ${elapsed}s"
      log "  ✅ omv-vmmanager updated and running on Docker"
      log "     https://$(hostname -I | awk '{print $1}')  (self-signed, accept once)"
      exit 0
      ;;
    starting|unhealthy|unknown)
      :
      ;;
  esac
  sleep "$HEALTH_INTERVAL"
  elapsed=$((elapsed + HEALTH_INTERVAL))
done

# 5. Health check failed: roll back.
fail_status="$status"
log "  ❌ health check did not become 'healthy' within ${HEALTH_TIMEOUT}s (last status: $fail_status)"
log "  rolling back to previous image..."
docker load -i "$BACKUP_DIR/omv-vmmanager.before-rollback.${TS}.tar.gz"
docker compose up -d backend
log "  previous image restored. Inspect with: docker logs omv-vmmanager"
exit 1
