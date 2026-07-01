# syntax=docker/dockerfile:1.7
# Multi-stage build for omv-vmmanager backend with embedded Svelte frontend.
# Stage 1: golang:1.25-trixie + libvirt-dev to produce a CGO binary.
# Stage 2: debian:13-slim + libvirt runtime libs + qemu/swtpm/ovmf.

# -------- Stage 1: build --------
FROM golang:1.25-trixie AS builder

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        libvirt-dev pkg-config gcc libc6-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy backend module + sources.
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN go mod download

COPY backend/ ./
# Drop the embedded frontend dist (re-populated below from frontend/dist at the repo root).
RUN rm -rf internal/frontend/dist

# Re-populate the embed directory with the freshly built frontend.
# Build context is the repo root, so frontend/dist/ is at /src/../frontend/dist.
# We stage both the backend sources and the frontend dist at /src so the COPY path is portable.
WORKDIR /src
COPY frontend/dist/ ./frontend-dist-staging/
WORKDIR /src/backend
RUN mkdir -p internal/frontend/dist && cp -r /src/frontend-dist-staging/* internal/frontend/dist/

# Build a stripped CGO binary that statically links libvirt client.
# VERSION and BUILD_TIME are stamped into the binary so the running
# container reports its real version in /api/system/version and the
# structured logs. CI (and `docker compose build`) pass these as
# build args; if you `docker build` manually, the defaults are
# "dev" and "unknown" (same as `make build` with no git tags).
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=1 go build -trimpath \
        -ldflags "-s -w -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}'" \
        -o /out/omv-vmmanager ./cmd/server

# -------- Stage 2: runtime --------
FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive

# Runtime deps: libvirt clients + qemu + swtpm + OVMF firmware + minimal net tools.
# curl + ca-certificates are bundled so operators can do quick API
# smoke tests from inside the container (e.g.
#   docker exec webvm-backend curl -H 'Authorization: Bearer ...' http://127.0.0.1:8080/...
# which we use in the smoke tests after deploys).
RUN apt-get update && apt-get install -y --no-install-recommends \
        libvirt-clients \
        qemu-system-x86 \
        qemu-utils \
        swtpm \
        ovmf \
        iproute2 \
        curl \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/omv-vmmanager /usr/local/bin/omv-vmmanager

# Backup script. The Docker compose file bind-mounts the host's
# /mnt/vmmanager-backup (SMB share) into the container at the same
# path, so the script can run identically inside the container
# and on a systemd host. Mount detection (mountpoint -q) is done
# at runtime, not build time.
COPY scripts/vmmanager-backup.sh /usr/local/bin/vmmanager-backup.sh
RUN chmod 0755 /usr/local/bin/vmmanager-backup.sh

# Pre-create the data dir layout that the host bind-mount will overlay.
# Bind mount on docker-compose wins, so this only matters for `docker run` without -v.
RUN mkdir -p /opt/openmediavault/vmmanager/pools/ISOS /opt/openmediavault/vmmanager/pools/vmmanager-disks /opt/openmediavault/vmmanager/covers /opt/openmediavault/vmmanager/logs /mnt/vmmanager-backup

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/omv-vmmanager"]
