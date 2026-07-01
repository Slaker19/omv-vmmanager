# AGENTS.md — Notas para agentes de código

omv-vmmanager es un fork de [Slaker19/webvm](https://github.com/Slaker19/webvm) adaptado para correr como plugin de OpenMediaVault (OMV). La arquitectura es idéntica — Go backend con frontend Svelte embebido, libvirt para VM management — pero los defaults de paths, branding, y binarios están renombrados.

## Diferencias clave vs webVM original

| Concepto | webVM original | omv-vmmanager |
|----------|---------------|---------------|
| Binario | `webvm-server` / `webvm-backend` | `omv-vmmanager` |
| Servicio systemd | `webvm-backend.service` | `omv-vmmanager.service` |
| DATA_DIR (OMV) | `/opt/webVM` | `/opt/openmediavault/vmmanager` |
| DATA_DIR (bare-metal) | `/opt/webVM` | `/opt/omv-vmmanager` |
| Disk pool libvirt | `webvm-disks` | `vmmanager-disks` |
| Backup script | `webvm-backup.sh` | `vmmanager-backup.sh` |
| Backup filename pattern | `webvm-<host>-<ts>` | `vmmanager-<host>-<ts>` |
| Marca UI | "WebVM Manager" | "OMV VM Manager" |
| Repo destino | `github.com/Slaker19/webvm` | `github.com/omv-vmmanager/omv-vmmanager` |
| XML metadata namespace | `https://webvm.local/ns` | `https://webvm.local/ns` (kept stable for backward compat) |

## Arquitectura de almacenamiento

El backend **no usa** los pools por defecto de libvirt (`/var/lib/libvirt/images`, `/var/lib/libvirt/isos`). En su lugar crea y administra pools propios bajo `DATA_DIR/pools`:

| Propósito | Nombre libvirt | Ruta (default en OMV) | Ruta (default bare-metal) |
|-----------|----------------|------------------------|---------------------------|
| Discos de VM | `vmmanager-disks` | `/opt/openmediavault/vmmanager/pools/vmmanager-disks` | `/opt/omv-vmmanager/pools/vmmanager-disks` |
| ISOs | `ISOS` | `/opt/openmediavault/vmmanager/pools/ISOS` | `/opt/omv-vmmanager/pools/ISOS` |

### Campo `purpose`

Cada pool tiene un propósito: `disk` (VDI) o `iso`. Se guarda en `{DATA_DIR}/pool-purposes.json`.

- Backend: `backend/internal/libvirt/pool_purpose.go` maneja carga/guardado e inferencia.
- Inferencia por nombre: cualquier nombre que contenga `iso` → `iso`; el resto → `disk`.
- Modelo `StoragePool` expone `purpose` en JSON.
- Al crear un pool se puede enviar `purpose` (`disk` o `iso`).

### Constantes relevantes

- `backend/internal/config/config.go` define `DiskPoolName = "vmmanager-disks"` y `ISOPoolName = "ISOS"`.
- `backend/internal/libvirt/pool_purpose.go` define `PoolPurposeDisk = "disk"` y `PoolPurposeISO = "iso"`.
- `Config.DiskPoolPath()` e `Config.ISOPoolPath()` devuelven las rutas completas.

### Creación de pools

`Connector.EnsureDefaults()` (`backend/internal/libvirt/connect.go`) crea los directorios y define los pools en libvirt al arrancar. Requiere que el proceso tenga permiso de escritura sobre `DATA_DIR`.

### Detección de OMV

`config.isOMVHost()` (en `config.go`) chequea la presencia de `/etc/openmediavault/config.xml` y retorna el `DATA_DIR` correspondiente. Esto permite que el mismo binario funcione tanto en OMV como en bare-metal sin flags.

## APIs de ISO con selección de pool

- `GET /api/storage/isos?pool=ISOS`
- `POST /api/storage/upload-iso` (campo multipart `pool`, default `ISOS`)
- `POST /api/storage/upload-iso/raw?pool=ISOS`
- `POST /api/storage/download-iso` (`pool` en body, default `ISOS`)
- `DELETE /api/storage/isos/{pool}/{name}`

`ISOScanResult` y `DownloadISORequest` incluyen el campo `pool`.

## Discos por defecto

- Creación de VM: default `vmmanager-disks`.
- API de volúmenes (`/api/storage/volumes`): default `vmmanager-disks`.
- Importar VM/OVA: default `vmmanager-disks`.

## Frontend

- `auth.svelte.js`: `listISOs(pool)`, `uploadISO(file, onProgress, pool)`, `downloadISO(url, name, pool)`, `deleteISO(name, pool)`.
- `Storage.svelte`: selector de pool en la sección ISO Library.
- `Storage.svelte`: los pools de tipo `iso` son de solo lectura en la sección Volumes: no se muestra el botón de crear volumen ni el de resize, y el backend rechaza `CreateStorageVolume` / `ResizeStorageVolume` sobre pools ISO.
- `VmCreate.svelte`: default de `storagePool` es `vmmanager-disks` si existe.

## Build

Después de cambiar el frontend:

```bash
cd frontend && npm run build
cp -r dist ../backend/internal/frontend/dist
cd ../backend && go build -o omv-vmmanager ./cmd/server
```

**Atajo**: `make build` hace los tres pasos y estampa la versión
(`git describe --tags --always` o el contenido de `VMMANAGER_VERSION`,
fallback a `dev`) en el binario vía `ldflags`. Lo mismo hace
`make docker-build` para las imágenes Docker (`omv-vmmanager-backend:latest`
+ `omv-vmmanager-frontend:latest`), con el mismo triple tag que el CI
(`latest`, la versión humana, el SHA corto).

### Push a GHCR (release)

```bash
export GHCR_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
export GHCR_ORG=omv-vmmanager
echo "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_ORG" --password-stdin
make docker-push
```

`make docker-push` produce 6 tags en
`ghcr.io/omv-vmmanager/omv-vmmanager-{backend,frontend}`: `:latest`,
`:<version>`, `:<sha>`. Coincide con el CI, así que el deploy
de un release es `docker compose pull && docker compose up -d`
desde un `docker-compose.yml` que apunte a la imagen de GHCR
en lugar de construir local.

### Empaquetado Debian (plugin OMV)

```bash
# Una vez que el directorio debian/ esté presente (Phase 1):
make deb
```

Produce un `.deb` instalable en OMV 7 con dependencias declaradas
en `debian/control`, Salt states en `usr/share/openmediavault/salt/omv-vmmanager/`,
PHP RPC en `usr/share/openmediavault/engined/rpc/vmmanager.inc`, y
manifest de UI en `usr/share/openmediavault/workbench/navigation.d/`.

### Tests + lint (antes de push)

```bash
cd backend && go test ./... && go vet ./...
cd ../frontend && npm run lint && npm run build
```

## Despliegue

`DATA_DIR` debe existir y ser escribible por el usuario que corre el backend. Hay tres modos de instalación:

**1. Plugin OMV (recomendado en OMV)**

```bash
sudo apt install ./openmediavault-vmmanager_1.0.0_all.deb
omv-salt deploy run vmmanager
```

El .deb instala el binario, escribe `/etc/default/omv-vmmanager`,
configura el service unit, y crea `/opt/openmediavault/vmmanager/`.

**2. Standalone en bare-metal Debian**

```bash
sudo /opt/omv-vmmanager/scripts/setup.sh
```

Crea `/opt/omv-vmmanager/` con un layout paralelo al OMV.

**3. Docker**

```bash
git clone https://github.com/omv-vmmanager/omv-vmmanager
cd omv-vmmanager
make docker
```

Levanta el backend y Caddy sidecar; DATA_DIR es `/opt/omv-vmmanager`
dentro del container (bind-mount `/opt/omv-vmmanager` desde el host).

Para OMV con Docker, override `VMMANAGER_DATA_DIR=/opt/openmediavault/vmmanager`.

## API: `/api/host/interfaces` filtra bridges

`/api/host/interfaces` excluye deliberadamente cualquier interfaz que tenga un directorio `/sys/class/net/<name>/bridge` — eso incluye `br0`, `virbr0`, y cualquier Linux bridge. Razón: esa ruta lista NICs candidatas a bridgear, no bridges existentes. Los bridges ya configurados se consultan en `/api/host/bridges`. Si un operador no ve `br0` en la respuesta, está donde debe.

## Logs estructurados

El backend usa `log/slog` con salida a `stderr` (capturado por `docker logs` o journald). Si se setea `VMMANAGER_LOG_FILE`, además escribe en formato idéntico a un archivo local — típicamente `{DATA_DIR}/logs/backend.log`. La pestaña "Logs" de la UI (`GET /api/system/logs`) intenta leer en este orden: 1) `VMMANAGER_LOG_FILE`, 2) `/var/log/vmmanager/backend.log` (legacy systemd), 3) `journalctl -u omv-vmmanager` (systemd). En Docker, solo el path 1 funciona porque `journalctl` no está instalado en el container. El response incluye `X-Log-Source` para que se sepa qué fuente se usó. La rotación se delega al `logrotate` del host (`scripts/omv-vmmanager.logrotate` cubre ambos paths).

## Backup v2 — gotchas (Phase I)

`backend/internal/backupstore/runner.go` tiene tres sutilezas
descubiertas durante el post-deploy. Si volvés a tocar el runner,
releerlas primero:

1. **El tar usa `-C r.dataDir`, no `-C filepath.Dir(r.dataDir)`.**
   El bug original (`-C /opt`) hacía que los basenames de
   metadata (`users.json`, etc.) se buscasen como `/opt/users.json`
   y no se encontrasen → `tar: exit status 2` → 500 al usuario.
   La regla: cualquier path que se le pase a tar tiene que existir
   cuando se le prepende `-C`. El fix actual pasa basenames y
   `--transform 's,^,webVM/,'`, así que el archive contiene
   `webVM/users.json` (que es lo que `RestoreBackup` espera).

2. **`RecordJob` ahora devuelve `(Job, error)`.** El caller
   (`Runner.RunOnce`) reasigna `job, err := r.store.RecordJob(job)`
   y trabaja con el `Job` retornado. Sin esa reasignación, el
   `job.ID` del caller se queda en `""` (porque `RecordJob` muta
   una copia local), y el `UpdateJob` posterior falla con
   "job not found" silenciosamente → el job queda `running`
   en disco aunque el cuerpo del response diga `error`. Si
   revertís el cambio de signature tenés que re-aplicar la
   reasignación en el runner o vas a reintroducir el bug.

3. **`--exclude` de GNU tar no soporta size filters.** El
   `--exclude=*.100M-and-larger` original era un no-op silencioso.
   El filtro de tamaño es Go-side en `collectVMFiles` (cada
   candidato se `os.Stat`'a antes de agregarse a la lista). Si
   necesitás re-introducir el filtro de tamaño, hacelo en Go,
   no en el comando tar.

`Store.New` llama `sweepStuckJobs(s)` después de cargar de disco.
Cubre el caso de un `kill -9` entre `RecordJob` y `UpdateJob`
dejando un job `running` para siempre en `jobs.json`. No tocar
sin un test que cubra el escenario — la sweep es la única
recuperación que existe.
