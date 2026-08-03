#!/bin/bash
# Production deploy for a specific version tag.
#
# This is the script the deploy key's forced command in /root/.ssh/authorized_keys
# points at. It is installed to /usr/local/bin/deploy-chevalet.sh; this copy in the
# repo is the source of truth — see "Installing" below.
#
# WHY A SCRIPT INSTEAD OF A FORCED COMMAND STRING: the previous forced command was
# `git pull --ff-only && docker compose up -d --build`, which always deploys the
# tracked *branch*. That silently ignores which version CI asked for — a "deploy
# v1.2.0" run would ship whatever main happened to point at. Deploying an exact tag
# needs the tag as input, so the forced command has to be a script that reads it.
#
# The whole point of a forced command is that the client cannot choose what runs, so
# the input arriving in SSH_ORIGINAL_COMMAND is treated as untrusted: the only thing
# ever extracted from it is a tag name matching ^v[0-9]+\.[0-9]+\.[0-9]+$, and that
# string is never eval'd or interpolated into a shell command. Anything else is
# refused. A caller holding the key can therefore only ever deploy some existing
# semver tag of this repo — nothing else.
#
# Usage (from CI, over ssh):  deploy vX.Y.Z
#
# Installing (or updating) on the server:
#   install -m 755 /opt/chevalet-go-staging/deploy/go/deploy-tag.sh \
#           /usr/local/bin/deploy-chevalet.sh
# and the authorized_keys entry for the deploy key must read:
#   command="/usr/local/bin/deploy-chevalet.sh",no-agent-forwarding,\
#   no-X11-forwarding,no-port-forwarding,no-pty ssh-ed25519 AAAA... github-actions-deploy@chevaletanonbot
set -euo pipefail

REPO=/opt/chevalet-go-staging
COMPOSE_DIR="$REPO/deploy/go"
COMPOSE_FILE=docker-compose.cutover.yml
PROJECT=go
CONTAINER=chevalet-go-bot-prod
IMAGE=chevalet-go-bot
LOGFILE=/var/log/chevalet-deploy.log
KEEP_IMAGES=5          # versioned images retained for instant rollback
HEALTH_TIMEOUT=180     # seconds to wait for the container to report healthy
POLL_TIMEOUT=90        # seconds to wait for proof that Telegram polling started
SETTLE_SECONDS=20      # quiet period used to detect a crash-loop after startup

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOGFILE"; }
die() { log "FAILED: $*"; exit 1; }

# ---------------------------------------------------------------- parse input
# drone-ssh (appleboy/ssh-action) sends the workflow's `script` as the original
# command, and may pass it as several lines. Scan the lines for one that is
# exactly `deploy vX.Y.Z` and ignore everything else, rather than trusting the
# whole payload.
TAG=""
while IFS= read -r line; do
    if [[ "$line" =~ ^[[:space:]]*deploy[[:space:]]+(v[0-9]+\.[0-9]+\.[0-9]+)[[:space:]]*$ ]]; then
        TAG="${BASH_REMATCH[1]}"
        break
    fi
done <<< "${SSH_ORIGINAL_COMMAND:-}"

if [[ -z "$TAG" ]]; then
    log "REFUSED: expected 'deploy vX.Y.Z', got: ${SSH_ORIGINAL_COMMAND:-<empty>}"
    echo "refused: this key only deploys a semver tag; expected 'deploy vX.Y.Z'" >&2
    exit 2
fi

log "=== deploy requested: $TAG ==="
cd "$REPO"

# ------------------------------------------------- resolve the requested tag
git fetch --tags --prune --force origin >>"$LOGFILE" 2>&1 || die "git fetch failed"

TARGET_COMMIT=$(git rev-parse --verify --quiet "refs/tags/${TAG}^{commit}") \
    || die "tag $TAG does not exist on origin"

# Remember where to go back to if the new version turns out to be unhealthy.
PREV_COMMIT=$(git rev-parse HEAD)
PREV_IMAGE=$(docker inspect --format '{{.Config.Image}}' "$CONTAINER" 2>/dev/null || echo "")
log "current: ${PREV_COMMIT:0:7} (image ${PREV_IMAGE:-none}) -> target: $TAG (${TARGET_COMMIT:0:7})"

if [[ "$PREV_COMMIT" == "$TARGET_COMMIT" ]]; then
    log "note: $TAG is the commit already checked out; redeploying anyway"
fi

# ----------------------------------------------------------------- roll forward
# deploy_commit <commit> <image_tag> [extra compose args...]
deploy_commit() {
    local commit="$1" image_tag="$2"
    shift 2
    git checkout --detach --force "$commit" >>"$LOGFILE" 2>&1 \
        || return 1
    cd "$COMPOSE_DIR"
    # IMAGE_TAG is consumed by docker-compose.cutover.yml, so each version keeps
    # its own image and a rollback can reuse one instead of rebuilding.
    IMAGE_TAG="$image_tag" docker compose -p "$PROJECT" -f "$COMPOSE_FILE" \
        up -d "$@" >>"$LOGFILE" 2>&1 || { cd "$REPO"; return 1; }
    cd "$REPO"
}

# An image already built for this tag means this is a rollback/redeploy: reuse it
# and skip the rebuild, which is both far faster and avoids depending on the
# build succeeding again right now.
BUILD_ARGS=(--build)
if docker image inspect "${IMAGE}:${TAG}" >/dev/null 2>&1; then
    BUILD_ARGS=()
    log "image ${IMAGE}:${TAG} already present — reusing it, skipping rebuild"
fi

deploy_commit "$TARGET_COMMIT" "$TAG" "${BUILD_ARGS[@]}" || die "build/up failed for $TAG"

# ------------------------------------------------------------- verify health
log "waiting for $CONTAINER to report healthy (timeout ${HEALTH_TIMEOUT}s)"
healthy=false
for _ in $(seq 1 "$HEALTH_TIMEOUT"); do
    state=$(docker inspect --format '{{.State.Health.Status}}' "$CONTAINER" 2>/dev/null || echo missing)
    running=$(docker inspect --format '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)
    if [[ "$state" == healthy ]]; then healthy=true; break; fi
    if [[ "$running" != true ]]; then log "container stopped running (health=$state)"; break; fi
    sleep 1
done

rollback() {
    log "$1 — rolling back to ${PREV_COMMIT:0:7}"
    docker logs "$CONTAINER" --tail 30 >>"$LOGFILE" 2>&1 || true
    local prev_tag="${PREV_IMAGE##*:}"
    [[ -z "$prev_tag" || "$prev_tag" == "$PREV_IMAGE" ]] && prev_tag=prod
    # No --build on the way back: the previous image is already on disk, and a
    # rollback must not depend on a build succeeding.
    if deploy_commit "$PREV_COMMIT" "$prev_tag"; then
        log "rolled back to ${PREV_COMMIT:0:7} (image ${IMAGE}:${prev_tag})"
    else
        log "ROLLBACK FAILED — manual intervention needed on $CONTAINER"
    fi
    die "$TAG was not verified live; production left on the previous version"
}

if [[ "$healthy" != true ]]; then
    rollback "UNHEALTHY after deploy of $TAG"
fi

# --------------------------------------------------- verify it is actually LIVE
# `healthy` is necessary but NOT sufficient. The healthcheck is a bare TCP connect
# to the health port, and main.go opens that listener BEFORE Run() calls
# StartPolling — so a container reports healthy before it has ever reached
# Telegram. If polling then fails, main exits, Docker's restart policy loops it,
# and without this check the deploy would already have been reported green.
#
# So require positive evidence from THIS container start: the "bot polling" line
# (which is only logged after StartPolling succeeded, i.e. Telegram accepted
# getUpdates), then confirm it stays up rather than crash-looping.
started_at=$(docker inspect --format '{{.State.StartedAt}}' "$CONTAINER")
restarts_before=$(docker inspect --format '{{.RestartCount}}' "$CONTAINER")

log "waiting for evidence of live polling (timeout ${POLL_TIMEOUT}s)"
polling=false
for _ in $(seq 1 "$POLL_TIMEOUT"); do
    if docker logs "$CONTAINER" --since "$started_at" 2>&1 | grep -q 'msg="bot polling"'; then
        polling=true; break
    fi
    [[ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER" 2>/dev/null)" != true ]] \
        && { log "container exited before it reported polling"; break; }
    sleep 1
done
[[ "$polling" == true ]] || rollback "$TAG never reported 'bot polling' (Telegram unreachable?)"

# A crash-loop can still look healthy in snapshots, because each fresh start
# re-opens the health port. Compare restart counts across a settle window.
log "settling ${SETTLE_SECONDS}s to rule out a crash-loop"
sleep "$SETTLE_SECONDS"
restarts_after=$(docker inspect --format '{{.RestartCount}}' "$CONTAINER")
running_now=$(docker inspect --format '{{.State.Running}}' "$CONTAINER" 2>/dev/null || echo false)

[[ "$running_now" == true ]] || rollback "$TAG stopped running during the settle window"
if [[ "$restarts_after" != "$restarts_before" ]]; then
    rollback "$TAG is crash-looping (restart count $restarts_before -> $restarts_after)"
fi
if docker logs "$CONTAINER" --since "$started_at" 2>&1 | grep -qE 'panic: |level=FATAL'; then
    rollback "$TAG panicked after starting"
fi

# Deliberately NOT a rollback trigger: a single level=ERROR right after start is
# usually ordinary traffic (e.g. a send to a user who blocked the bot). Surfacing
# it beats auto-reverting a good release on a benign error.
errs=$(docker logs "$CONTAINER" --since "$started_at" 2>&1 | grep -c 'level=ERROR' || true)
[[ "$errs" -gt 0 ]] && log "note: $errs level=ERROR line(s) since start — not treated as deploy failure"

log "verified live: polling up, no crash-loop, no panic"

# `:prod` stays a moving pointer at whatever is live, so CUTOVER.md's manual
# commands and anything else referencing :prod keep working.
docker tag "${IMAGE}:${TAG}" "${IMAGE}:prod"

# ------------------------------------------------------------------ housekeeping
# Keep the newest few versioned images so rollbacks stay instant; drop the rest.
docker images --format '{{.Repository}}:{{.Tag}}\t{{.CreatedAt}}' \
    | grep -E "^${IMAGE}:v[0-9]+\.[0-9]+\.[0-9]+\b" \
    | sort -k2 -r | tail -n +$((KEEP_IMAGES + 1)) | cut -f1 \
    | while read -r old; do
          log "pruning old image $old"
          docker rmi "$old" >>"$LOGFILE" 2>&1 || true
      done

log "=== deployed $TAG (${TARGET_COMMIT:0:7}) successfully, $CONTAINER healthy ==="
echo "deployed $TAG (${TARGET_COMMIT:0:7}); $CONTAINER healthy"
