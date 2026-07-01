# omv-vmmanager

Plugin de gestión de máquinas virtuales para **OpenMediaVault** (libvirt + QEMU/KVM), basado en [webVM](https://github.com/Slaker19/webvm).

[![CI](https://github.com/omv-vmmanager/omv-vmmanager/actions/workflows/ci.yml/badge.svg)](https://github.com/omv-vmmanager/omv-vmmanager/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.25-blue)
![Svelte](https://img.shields.io/badge/Svelte-5-orange)
![License](https://img.shields.io/badge/license-MIT-green)

## Instalación

```bash
git clone https://github.com/omv-vmmanager/omv-vmmanager
cd omv-vmmanager
sudo ./install.sh
```

El script detecta automáticamente:

| Entorno | Qué instala |
|---------|-------------|
| **OpenMediaVault** | Plugin `.deb` nativo — aparece en Services > Virtual Machines |
| **Debian/Ubuntu bare-metal** | Binario + systemd service + (opcional) Caddy HTTPS |
| **Docker** | Contenedores backend + Caddy reverse proxy |

## Build manual

```bash
make build          # compila backend (Go) + frontend (Svelte)
make install        # instala binario + systemd service + logrotate
make install-caddy  # (opcional) proxy HTTPS con Caddy
make docker         # modo Docker
```

## Detalles técnicos

| Componente | Puerto | Descripción |
|------------|--------|-------------|
| Go backend | `port` (8080) | API REST + frontend Svelte embebido |
| Caddy | `httpsPort` (8443) | Reverse proxy público (TLS o HTTP según modo) |
| nginx (OMV) | `port` (8080) | Proxy interno desde `/vmmanager/` |

## Enlaces

- [Documentación del plugin OMV](docs/OMV-PLUGIN.md)
- [Guía de desarrollo](docs/DEV.md) (agentes de código: `AGENTS.md`)
- [API REST](docs/API.md)
