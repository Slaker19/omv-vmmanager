.PHONY: all install-deps build run dev stop docker docker-build docker-push docker-config docker-down docker-logs clean install uninstall update status logs install-systemd install-systemd-force install-caddy install-all regen-cert rollback deb

# Version: prefer git tag/describe, fall back to "dev" for local builds.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w \
  -X 'main.Version=$(VERSION)' \
  -X 'main.BuildTime=$(BUILD_TIME)'

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
SYSTEMDDIR ?= /etc/systemd/system
LOGROTATEDIR ?= /etc/logrotate.d

# On OMV hosts the data dir lives under /opt/openmediavault/ so OMV's
# shared-folder / SMB / quota machinery covers the pools. On bare-metal
# Debian the install dir is also the data dir.
ifneq ("$(wildcard /etc/openmediavault/config.xml)","")
DATADIR ?= /opt/openmediavault/vmmanager
else
DATADIR ?= /opt/omv-vmmanager
endif
LOGDIR ?= /var/log/vmmanager

all: build

# ---- Dependencies ----

install-deps:
	@echo "Detecting distro..."
	@if ! command -v go >/dev/null 2>&1; then \
		echo "Installing Go 1.25..."; \
		curl -sL https://go.dev/dl/go1.25.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf -; \
		echo 'export PATH=$$PATH:/usr/local/go/bin:$$HOME/go/bin' >> $$HOME/.bashrc; \
		export PATH=$$PATH:/usr/local/go/bin:$$HOME/go/bin; \
		echo "Go installed. Run: source ~/.bashrc"; \
	else \
		echo "Go already installed: $$(go version)"; \
	fi; \
	if command -v apt >/dev/null 2>&1; then \
		echo "Debian/Ubuntu detected"; \
		sudo apt update && sudo apt install -y \
			libvirt-daemon-system libvirt-dev qemu-system-x86 \
			swtpm ovmf virtinst bridge-utils \
			curl ca-certificates nodejs npm git \
			gcc libc6-dev make; \
	elif command -v pacman >/dev/null 2>&1; then \
		echo "Arch Linux detected"; \
		sudo pacman -S --needed --noconfirm \
			libvirt qemu-full swtpm edk2-ovmf dmidecode \
			curl nodejs npm git base-devel go; \
	else \
		echo "Unsupported distro. Install dependencies manually."; \
		exit 1; \
	fi; \
	echo "Enabling libvirtd..."; \
	sudo systemctl enable --now libvirtd; \
	echo "Done!"

# ---- Build ----

build-frontend:
	cd frontend && npm ci 2>/dev/null || npm install
	cd frontend && npm run build
	@echo "Frontend built -> frontend/dist/"

build-backend: build-frontend
	cd backend && \
	  rm -rf internal/frontend/dist && \
	  mkdir -p internal/frontend/dist && \
	  cp -r ../frontend/dist/* internal/frontend/dist/
	cd backend && CGO_ENABLED=1 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o omv-vmmanager ./cmd/server/
	@echo "Backend built -> backend/omv-vmmanager (v$(VERSION))"

build: build-backend
	@echo ""
	@echo "Build complete (v$(VERSION))."
	@echo "Run 'make install' to install as a systemd service."

# ---- Install / Uninstall (systemd) ----

install: build
	@echo "Installing omv-vmmanager..."
	@echo "  binary  -> $(BINDIR)/omv-vmmanager"
	@echo "  data    -> $(DATADIR)"
	@echo "  logs    -> $(LOGDIR)"
	@echo "  service -> $(SYSTEMDDIR)/omv-vmmanager.service"
	sudo install -d $(BINDIR)
	sudo install -d $(DATADIR)
	sudo install -d $(LOGDIR)
	# Put the user in the libvirt group so manual 'virsh' / 'virt-manager'
	# works after they re-login. The service itself runs as root.
	@if ! id -nG "$$USER" 2>/dev/null | tr ' ' '\n' | grep -qx libvirt; then \
		echo "  adding $$USER to 'libvirt' group (re-login required for manual virsh)"; \
		sudo usermod -aG libvirt "$$USER" || true; \
	fi
	sudo install -m 0755 backend/omv-vmmanager $(BINDIR)/omv-vmmanager
	sudo install -m 0644 scripts/omv-vmmanager.service $(SYSTEMDDIR)/omv-vmmanager.service
	sudo install -m 0644 scripts/omv-vmmanager.logrotate $(LOGROTATEDIR)/vmmanager 2>/dev/null || true
	sudo systemctl daemon-reload
	sudo systemctl enable --now omv-vmmanager
	@echo ""
	@echo "  ✅ omv-vmmanager installed and running"
	@echo "  Open http://localhost:8080 in your browser"
	@echo "  Login: admin / admin"
	@echo "  Service: systemctl status omv-vmmanager"

install-systemd: build-backend
	@echo "Installing omv-vmmanager backend (with health-check + auto-rollback)..."
	sudo install -d $(BINDIR)
	# Move the currently-running binary aside BEFORE we install the
	# new one. If anything goes wrong (service fails to start,
	# health check fails), `make rollback` puts this back.
	if [ -f $(BINDIR)/omv-vmmanager ] && [ ! -f $(BINDIR)/omv-vmmanager.previous ]; then \
		sudo cp -a $(BINDIR)/omv-vmmanager $(BINDIR)/omv-vmmanager.previous; \
		echo "  saved previous binary to $(BINDIR)/omv-vmmanager.previous"; \
	fi
	sudo install -m 0755 backend/omv-vmmanager $(BINDIR)/omv-vmmanager
	sudo install -d $(SYSTEMDDIR)
	sudo install -m 0644 scripts/omv-vmmanager.service $(SYSTEMDDIR)/omv-vmmanager.service
	sudo install -m 0644 scripts/omv-vmmanager.logrotate $(LOGROTATEDIR)/vmmanager 2>/dev/null || true
	sudo systemctl daemon-reload
	sudo systemctl restart omv-vmmanager
	@echo ""
	@echo "  waiting for backend to come up..."
	@for i in $$(seq 1 20); do \
		if curl -fsS -m 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1; then \
			echo "  health check passed after $${i}s"; \
			echo ""; \
			echo "  ✅ omv-vmmanager updated and running"; \
			echo "  To rollback: make rollback"; \
			echo "  To add HTTPS: make install-caddy"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "  ❌ health check did not pass within 20s — auto-rolling back"; \
	sudo make --no-print-directory rollback; \
	exit 1

# install-systemd-force: skip the health check. Use only when you
# know the new binary is fine and the loopback port is unreachable
# (e.g. the binary binds to a different port for some reason).
install-systemd-force: build-backend
	@echo "Installing omv-vmmanager backend (FORCE — no health check)..."
	sudo install -d $(BINDIR)
	if [ -f $(BINDIR)/omv-vmmanager ] && [ ! -f $(BINDIR)/omv-vmmanager.previous ]; then \
		sudo cp -a $(BINDIR)/omv-vmmanager $(BINDIR)/omv-vmmanager.previous; \
	fi
	sudo install -m 0755 backend/omv-vmmanager $(BINDIR)/omv-vmmanager
	sudo install -d $(SYSTEMDDIR)
	sudo install -m 0644 scripts/omv-vmmanager.service $(SYSTEMDDIR)/omv-vmmanager.service
	sudo install -m 0644 scripts/omv-vmmanager.logrotate $(LOGROTATEDIR)/vmmanager 2>/dev/null || true
	sudo systemctl daemon-reload
	sudo systemctl restart omv-vmmanager
	@echo "  ✅ omv-vmmanager updated (no health check performed)"

# Install Caddy for HTTPS termination. Idempotent: re-running
# overwrites /etc/caddy/Caddyfile (backed up to .bak.pre-vmmanager on
# the first run) and reloads the service. Use SKIP_INSTALL=1 to
# only refresh the Caddyfile (e.g. after a custom-cert change).
install-caddy:
	@if [ "$$SKIP_INSTALL" = "1" ]; then \
		echo "Refreshing /etc/caddy/Caddyfile only (SKIP_INSTALL=1)..."; \
		sudo install -m 0644 scripts/Caddyfile /etc/caddy/Caddyfile; \
		sudo caddy validate --config /etc/caddy/Caddyfile; \
		sudo systemctl reload-or-restart caddy; \
	else \
		sudo scripts/install-caddy-systemd.sh; \
	fi

# Regenerate the self-signed cert (10-year, LAN IP + DNS:hostname SAN).
# Use when the host's IP changes or the cert expires.
regen-cert:
	sudo FORCE=1 scripts/generate-self-signed.sh
	sudo systemctl reload caddy

# One-shot: backend + caddy. Use on a fresh host.
install-all: install-systemd install-caddy
	@echo ""
	@echo "  ✅ omv-vmmanager fully installed (backend on :8080, https on :443)"
	@echo "  open https://$$(hostname -I | awk '{print $$1}')"

# Roll back to the previous binary (saved by install-systemd).
rollback:
	@if [ ! -f $(BINDIR)/omv-vmmanager.previous ]; then \
		echo "ERROR: $(BINDIR)/omv-vmmanager.previous not found" >&2; \
		echo "  Nothing to roll back to." >&2; \
		exit 1; \
	fi
	@echo "Rolling back omv-vmmanager backend..."
	sudo mv $(BINDIR)/omv-vmmanager.previous $(BINDIR)/omv-vmmanager
	sudo systemctl restart omv-vmmanager
	@for i in $$(seq 1 20); do \
		if curl -fsS -m 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1; then \
			echo "  ✅ rolled back; health check passed after $${i}s"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "  ❌ rolled back but health check still failing — check 'systemctl status omv-vmmanager'"; \
	exit 1

uninstall:
	@echo "Removing omv-vmmanager..."
	-sudo systemctl disable --now omv-vmmanager 2>/dev/null
	-sudo rm -f $(SYSTEMDDIR)/omv-vmmanager.service
	-sudo rm -f $(LOGROTATEDIR)/vmmanager
	-sudo rm -f $(BINDIR)/omv-vmmanager
	sudo systemctl daemon-reload
	@echo "Note: data in $(DATADIR) and logs in $(LOGDIR) were kept. Remove manually if desired."

# ---- Service management ----

status:
	@systemctl status omv-vmmanager --no-pager || true

logs:
	@journalctl -u omv-vmmanager -f --no-pager

# ---- Quick start (no install) ----

run:
	@if pgrep -f omv-vmmanager >/dev/null 2>&1; then \
		echo "omv-vmmanager is already running (pid: $$(pgrep -f omv-vmmanager | tr '\n' ' '))"; \
		echo "Run 'make stop' first if you want a fresh instance."; \
		exit 1; \
	fi
	@echo "Starting in background..."
	cd backend && nohup ./omv-vmmanager > backend.log 2>&1 &
	@echo "Backend PID: $$!  (logs: backend/backend.log)"
	@echo "Open http://localhost:8080"

stop:
	-pkill -f omv-vmmanager 2>/dev/null || true
	@echo "Stopped."

# ---- Development (hot reload, no embed) ----

dev-backend:
	cd backend && go run ./cmd/server/

dev-frontend:
	cd frontend && npm run dev

# ---- Docker ----

docker:
	docker compose up --build -d
	@echo ""
	@echo "  App:    http://localhost:8080"
	@echo "  Login:  admin / admin"

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Build the backend + frontend Docker images locally without
# starting any container. Tags both images with the human
# version (from `git describe`, falling back to the VMMANAGER_VERSION
# env var, then to "dev") and with `:latest`.
docker-build:
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "$${VMMANAGER_VERSION:-dev}"); \
	BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	echo "Building with VERSION=$$VERSION BUILD_TIME=$$BUILD_TIME"; \
	DOCKER_BUILDKIT=1 docker build \
	  --build-arg VERSION="$$VERSION" \
	  --build-arg BUILD_TIME="$$BUILD_TIME" \
	  -f backend/Dockerfile \
	  -t omv-vmmanager-backend:"$$VERSION" \
	  -t omv-vmmanager-backend:latest \
	  . && \
	DOCKER_BUILDKIT=1 docker build \
	  --build-arg VERSION="$$VERSION" \
	  --build-arg BUILD_TIME="$$BUILD_TIME" \
	  -f frontend/Dockerfile \
	  -t omv-vmmanager-frontend:"$$VERSION" \
	  -t omv-vmmanager-frontend:latest \
	  . && \
	echo "Images built:" && \
	docker images omv-vmmanager-backend omv-vmmanager-frontend --format "  {{.Repository}}:{{.Tag}}\t{{.Size}}"

# Build, tag, and push to GHCR. Requires a `GHCR_TOKEN` in the
# env. Each image gets three tags: :latest, the human version,
# and the short SHA.
docker-push: docker-build
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "$${VMMANAGER_VERSION:-dev}"); \
	SHA=$$(git rev-parse --short HEAD 2>/dev/null || echo "dev"); \
	if [ -z "$$GHCR_TOKEN" ]; then \
	  echo "GHCR_TOKEN is empty. Set it before running: export GHCR_TOKEN=ghp_xxx"; \
	  exit 1; \
	fi; \
	ORG=$${GHCR_ORG:-omv-vmmanager}; \
	echo "$$GHCR_TOKEN" | docker login ghcr.io -u $$ORG --password-stdin && \
	for IMG in omv-vmmanager-backend omv-vmmanager-frontend; do \
	  for TAG in "$$VERSION" latest "$$SHA"; do \
	    docker tag $$IMG:$$VERSION ghcr.io/$$ORG/$$IMG:$$TAG; \
	    docker push ghcr.io/$$ORG/$$IMG:$$TAG; \
	  done; \
	done && \
	echo "Pushed to GHCR. Deploy with:" && \
	echo "  docker compose pull && docker compose up -d"

# Validate that the compose file still parses.
docker-config:
	DOCKER_BUILDKIT=0 docker compose config --quiet

# ---- OMV 8 plugin .deb ----
DEB_PKG := openmediavault-vmmanager

deb:
	@test -d debian || { echo "ERROR: debian/ missing — Phase 2 not done." >&2; exit 1; }
	@echo "Building $(DEB_PKG) .deb via dpkg-buildpackage..."
	@make build
	@install -D -m 0755 backend/omv-vmmanager usr/sbin/omv-vmmanager
	@install -D -m 0644 scripts/omv-vmmanager.service usr/lib/systemd/system/omv-vmmanager.service
	dpkg-buildpackage -us -uc -b
	@echo ""
	@echo "  ✅ $(DEB_PKG) .deb built (see ../$(DEB_PKG)_*.deb)"
	@ls -la ../*.deb 2>/dev/null || true

deb-clean:
	rm -rf ../$(DEB_PKG)_* ../$(DEB_PKG)-*
	rm -f usr/sbin/omv-vmmanager usr/lib/systemd/system/omv-vmmanager.service

# ---- Clean ----

clean: stop
	rm -f backend/omv-vmmanager
	rm -rf frontend/dist backend/internal/frontend/dist
	@echo "Cleaned."
