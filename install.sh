#!/usr/bin/env bash
# omv-vmmanager — unified installer
# Auto-detects the environment and runs the correct installation method.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(git describe --tags --always 2>/dev/null || echo 'dev')"

# ── helpers ──────────────────────────────────────────────────────────
red()   { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
bold()  { printf "\033[1m%s\033[0m\n" "$*"; }

info()  { printf "  %s\n" "$*"; }
die()   { red "ERROR: $*"; exit 1; }

check_sudo() {
    sudo -v
    while true; do sudo -n true; sleep 300; kill -0 "$$" 2>/dev/null || exit; done 2>/dev/null &
}

# ── detect environment ───────────────────────────────────────────────
is_omv()          { [[ -f /etc/openmediavault/config.xml ]]; }
is_docker()       { command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; }
is_systemd()      { command -v systemctl >/dev/null 2>&1; }
has_libvirt()     { systemctl is-active libvirtd >/dev/null 2>&1; }
has_kvm()         { [[ -e /dev/kvm ]]; }

# ── install methods ──────────────────────────────────────────────────

install_omv_plugin() {
    bold "=== OMV plugin install ==="

    # Install build dependencies for compiling the .deb from source.
    # (When using a pre-built .deb from a release, these are not needed.)
    local build_deps=(
        build-essential dpkg-dev debhelper-compat
        git curl ca-certificates
        nodejs npm libvirt-dev gcc libc6-dev
        pkg-config
    )
    info "Installing build dependencies..."
    sudo apt update && sudo apt install -y "${build_deps[@]}"

    # Install Go 1.25+ if not present (needed for CGO backend).
    if ! command -v go >/dev/null 2>&1; then
        info "Installing Go 1.25..."
        curl -sL https://go.dev/dl/go1.25.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf -
        export PATH="$PATH:/usr/local/go/bin"
    fi
    # Ensure GOPATH/bin is in PATH (for tools like gomvpkg, etc.)
    export PATH="$PATH:$(go env GOPATH)/bin"

    local deb
    deb=$(find "$SCRIPT_DIR/.." "$SCRIPT_DIR" -maxdepth 1 -name 'openmediavault-vmmanager*.deb' 2>/dev/null | head -1)
    if [[ -z "$deb" ]]; then
        info "No .deb found — building..."
        make deb
        deb=$(find "$SCRIPT_DIR/.." "$SCRIPT_DIR" -maxdepth 1 -name 'openmediavault-vmmanager*.deb' 2>/dev/null | head -1)
    fi
    if [[ -z "$deb" ]]; then
        die "Could not find or build .deb package."
    fi
    info "Installing $deb ..."
    deb=$(readlink -f "$deb")
    sudo apt install -y "$deb"
    green "OMV plugin installed. Open OMV web UI → Services → Virtual Machines"
}

install_baremetal() {
    bold "=== Bare-metal install ==="

    if ! has_kvm; then
        die "KVM not available (/dev/kvm not found). This machine cannot run VMs."
    fi

    # Install deps if needed
    if ! command -v go >/dev/null 2>&1 || ! command -v node >/dev/null 2>&1; then
        info "Installing build dependencies..."
        if command -v apt >/dev/null 2>&1; then
            sudo apt update && sudo apt install -y \
                libvirt-daemon-system libvirt-dev qemu-system-x86 \
                swtpm ovmf virtinst bridge-utils \
                curl ca-certificates nodejs npm git \
                gcc libc6-dev make
        elif command -v pacman >/dev/null 2>&1; then
            sudo pacman -S --needed --noconfirm \
                libvirt qemu-full swtpm edk2-ovmf dmidecode \
                curl nodejs npm git base-devel go
        else
            die "Unsupported distro. Install Go 1.25+, Node.js, libvirt-dev manually."
        fi
    fi

    if ! has_libvirt; then
        info "Enabling libvirtd..."
        sudo systemctl enable --now libvirtd
    fi

    # Build & install
    info "Building omv-vmmanager (v$VERSION)..."
    make build
    make install

    # Optional Caddy
    if command -v caddy >/dev/null 2>&1; then
        info "Caddy detected — installing HTTPS proxy..."
        make install-caddy
    else
        bold "Caddy not installed. The backend is on http://localhost:8080"
        bold "To add HTTPS later: make install-caddy"
    fi

    green "omv-vmmanager installed and running."
    info "Web UI: http://$(hostname -I 2>/dev/null | awk '{print $1}'):8080"
    info "Login: admin / admin"
    info "Service: systemctl status omv-vmmanager"
}

install_docker() {
    bold "=== Docker install ==="
    if ! is_docker; then
        die "Docker not found. Install Docker first: https://docs.docker.com/engine/install/"
    fi
    info "Building and starting containers..."
    make docker
    sleep 3
    green "omv-vmmanager running in Docker."
    info "Web UI: http://localhost"
    info "Login: admin / admin"
    info "Logs: docker compose logs -f"
}

# ── main ─────────────────────────────────────────────────────────────

bold "omv-vmmanager installer v$VERSION"
echo ""

check_sudo

if is_omv; then
    bold "Detected: OpenMediaVault host"
    echo ""
    install_omv_plugin
elif is_docker; then
    bold "Detected: Docker available"
    echo ""
    # Ask: systemd or docker? Only if both available
    if is_systemd && has_libvirt; then
        echo "Both systemd (bare-metal) and Docker are available."
        echo "  1) Bare-metal install (recommended — native performance)"
        echo "  2) Docker install"
        read -rp "Choose [1/2] (default: 1): " choice
        case "$choice" in
            2|docker|Docker) install_docker ;;
            *) install_baremetal ;;
        esac
    else
        install_docker
    fi
elif is_systemd; then
    bold "Detected: Linux systemd host"
    echo ""
    install_baremetal
else
    die "No supported install method found. Install manually: see README.md"
fi
