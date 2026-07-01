# OMV 8 Plugin — omv-vmmanager

Instalación, configuración y arquitectura del plugin OMV 8 para omv-vmmanager.

---

## Requisitos

- **OpenMediaVault 8** (Sandworm) sobre Debian 13 (Trixie)
- Libvirt + QEMU/KVM habilitados en el host (BIOS/UEFI VT-x/VT-d)
- `openmediavault-sharerootfs` (dependencia de OMV para KVM plugins)

## Instalación

### Desde el .deb compilado

```bash
# Compilar el .deb (necesita Go 1.25, Node 22, libvirt-dev en build host)
make build
make deb

# Instalar
sudo dpkg -i ../openmediavault-vmmanager_*.deb
sudo apt-get install -f  # resuelve dependencias si falta algo
```

### Desde el repo fuente (desarrollo)

```bash
git clone https://github.com/omv-vmmanager/omv-vmmanager
cd omv-vmmanager
git checkout phase-2/omv-plugin-8
make install       # compila + instala binario + service + logrotate
```

## Verificación

```bash
# 1. Backend corriendo
systemctl status omv-vmmanager

# 2. API responde
curl -fsS http://127.0.0.1:8080/api/health | python3 -m json.tool

# 3. OMV UI muestra el menú
# Abrir: https://<ip-omv>
# Buscar "Virtual Machines" en el menú lateral (Services → Virtual Machines)

# 4. Iframe embebido
# Abrir: https://<ip-omv>/#/services/vmmanager/ui
```

## Desinstalación

```bash
sudo dpkg --purge openmediavault-vmmanager
# Opcional: borrar datos persistentes
sudo rm -rf /opt/openmediavault/vmmanager
```

Al purgar: el service se para, el confdb se limpia, el nginx reverse-proxy
se elimina, la API key se borra. El DATA_DIR (`/opt/openmediavault/vmmanager`)
se conserva por seguridad.

---

## Arquitectura del plugin

El plugin OMV es una **capa fina** sobre el backend Go existente.

```
┌─────────────────────────────────────────────────────────────────────┐
│  OMV 8 Web Workbench (Angular/ExtJS)                               │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  Sidebar: "Virtual Machines"                                  │   │
│  │    → /services/vmmanager/settings  (formPage)                 │   │
│  │    → /services/vmmanager/ui        (iframe → Go backend)      │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                          │                                          │
│                    OMV RPC (PHP)                                    │
│                    omv-engined                                      │
│                          │                                          │
│    ┌─────────────────────┼─────────────────────────────────┐       │
│    │                     │                                   │       │
│    │ conf.service.vmmanager  │  HTTP REST (via curl)         │       │
│    │ (confdb XML)            │  + Bearer API key             │       │
│    │                        │                                   │       │
│    └────────────────────────┼───────────────────────────────┘       │
│                              │                                       │
│                              ▼                                       │
│              omv-vmmanager Go backend (:8080)                        │
│              ├── libvirt (:16509 unix socket)                       │
│              └── DATA_DIR: /opt/openmediavault/vmmanager/            │
└─────────────────────────────────────────────────────────────────────┘
```

### Flujo de datos

1. **OMV UI** carga la página "Virtual Machines" → llama `GET /api/rpc/VmManager/getSettings` vía OMV RPC
2. **omv-engined** (PHP) ejecuta `OMVRpcServiceVmManager::getSettings()` → lee de `conf.service.vmmanager` en el confdb XML
3. Para endpoints proxy (`listVms`, `getHostStats`, etc.), el RPC PHP hace un HTTP GET al Go backend usando la API key pre-compartida
4. El Go backend responde con JSON que el RPC PHP reenvía a la OMV UI

### Auth: API key persistente

| Dato | Valor |
|------|-------|
| Ruta de la key | `/etc/openmediavault-vmmanager/apikey.key` |
| Permisos | `0600 root:root` |
| Cómo se genera | `postinst` al instalar el .deb (1ª vez) |
| Cómo se renueva | `rm /etc/openmediavault-vmmanager/apikey.key && dpkg-reconfigure openmediavault-vmmanager` |
| Alcance | `scopes: ["*"]` → todas las APIs del backend Go |

### Estructura del .deb

```
openmediavault-vmmanager_1.0.0_all.deb
├── usr/sbin/omv-vmmanager              ← binario Go (copiado de build/)
├── usr/lib/systemd/system/             ← unit file (copiado de scripts/)
├── usr/share/openmediavault/
│   ├── datamodels/                     ← confdb + RPC schemas
│   ├── confdb/create.d/                ← crea /config/services/vmmanager
│   ├── engined/{module,rpc,inc}/       ← PHP backend (RPC, listeners, logs)
│   └── workbench/{navigation,route,component,dashboard}.d/  ← UI YAMLs
├── srv/salt/omv/deploy/vmmanager/      ← Salt state (service, nginx, logrotate)
└── debian/{control,postinst,postrm,...} ← empaquetado
```

### Salt state: qué configura

| Archivo | Qué genera |
|---------|-----------|
| `10-omv.conf` (systemd drop-in) | Override DATA_DIR, BIND_ADDR, PORT, TimeoutStopSec |
| `openmediavault-vmmanager.conf` (nginx) | Reverse-proxy `/vmmanager/` → `127.0.0.1:8080` |
| `omv-vmmanager` (logrotate) | Rotación de `/var/log/vmmanager/*.log` |

---

## ConfDB: conf.service.vmmanager

| Campo | Tipo | Default | Descripción |
|-------|------|---------|-------------|
| `enable` | bool | `true` | Habilitar el servicio |
| `port` | int | `8080` | Puerto TCP del backend Go |
| `bind` | string | `127.0.0.1` | Bind address |
| `dataDir` | string | `/opt/openmediavault/vmmanager` | Directorio de datos |
| `https` | bool | `true` | Habilitar HTTPS (Caddy/nginx) |
| `caddyPort` | int | `8443` | Puerto HTTPS |
| `sharedFolder` | string | `""` | Shared folder de OMV (para backup) |
| `extraArgs` | string | `""` | Args extra pasados al binario |

---

## RPC Methods (PHP → Go backend)

### OMV-side (no proxy)

| Método | Auth | Qué hace |
|--------|------|----------|
| `getSettings` | viewer | Lee `conf.service.vmmanager` |
| `setSettings` | admin | Escribe confdb + trigger dirty |
| `getStatus` | viewer | `systemctl is-active` + `/api/health` |
| `openUI` | viewer | Devuelve la URL de la UI embebida |
| `restartService` | admin | `systemctl restart omv-vmmanager` |
| `getLogs` | viewer | `journalctl -u omv-vmmanager -n N` |
| `getDiskUsage` | viewer | `du` + `df` sobre DATA_DIR |

### Proxy al Go backend

| Método | Ruta Go | Qué devulve |
|--------|---------|-------------|
| `getHostStats` | `GET /api/host/stats` | CPU/RAM/disk del host |
| `listVms` | `GET /api/vms/` | Lista de VMs |
| `listStoragePools` | `GET /api/storage/pools` | Pools libvirt |
| `listNetworks` | `GET /api/networks/` | Redes virtuales |

---

## Workbench (UI)

| Archivo | Tipo | Qué muestra |
|---------|------|-------------|
| `navigation.d/services.vmmanager.yaml` | nav-item | Entrada "Virtual Machines" en el menú lateral |
| `route.d/services.vmmanager.yaml` | route | `/services/vmmanager` → navigationPage (sub-rutas: settings, ui) |
| `route.d/services.vmmanager.ui.yaml` | route | `/services/vmmanager/ui` → iframe page |
| `component.d/services.vmmanager.navigation-page.yaml` | navPage | Contenedor padre para las sub-rutas |
| `component.d/services.vmmanager.settings-form-page.yaml` | formPage | Formulario de configuración (enable, port, bind, etc.) |
| `component.d/services.vmmanager.embedded-ui-page.yaml` | textPage | `<iframe>` apuntando a `/vmmanager/` (nginx reverse-proxy) |
| `dashboard.d/services.vmmanager.dashboard.yaml` | datatable | Widget de dashboard mostrando lista de VMs |

---

## Troubleshooting

### Backend no arranca

```bash
# Verificar que el servicio existe
systemctl cat omv-vmmanager

# Verificar el drop-in OMV
cat /etc/systemd/system/omv-vmmanager.service.d/10-omv.conf

# Verificar logs
journalctl -u omv-vmmanager -n 30 --no-pager

# Verificar que el binario existe
ls -la /usr/sbin/omv-vmmanager
```

### API key perdida

```bash
# Forzar regeneración
rm /etc/openmediavault-vmmanager/apikey.key
dpkg-reconfigure openmediavault-vmmanager
# O reinstalar
sudo dpkg -i ../openmediavault-vmmanager_*.deb
```

### Iframe no carga (OMV UI)

1. Verificar que nginx reverse-proxy está configurado:
   ```bash
   cat /etc/nginx/openmediavault-webgui.d/openmediavault-vmmanager.conf
   ```
2. Verificar que nginx recargó:
   ```bash
   systemctl reload nginx
   ```
3. Verificar que el Go backend responde en el puerto:
   ```bash
   curl -fsS http://127.0.0.1:8080/api/health
   ```

### UI carga pero no hay datos (RPC falla)

```bash
# Verificar la API key
cat /etc/openmediavault-vmmanager/apikey.key

# Test manual de la key
APIKEY=$(cat /etc/openmediavault-vmmanager/apikey.key)
curl -fsS http://127.0.0.1:8080/api/vms/ -H "Authorization: Bearer $APIKEY"
```

### Revertir a sin OMV

```bash
# Desinstalar el plugin
sudo dpkg --purge openmediavault-vmmanager

# El binario Go sigue disponible como /usr/local/bin/omv-vmmanager
# (la instalación manual via make install)
# Para volver a instalar standalone:
cd omv-vmmanager && make install
```
