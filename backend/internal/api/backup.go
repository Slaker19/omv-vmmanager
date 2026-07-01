package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"omv-vmmanager/internal/backupstore"
	"omv-vmmanager/internal/config"
)

// --- Targets ---

type backupTargetCreateRequest struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Path     string   `json:"path"`
	VMFilter string   `json:"vm_filter"`
	VMIDs    []string `json:"vm_ids"`
}

func (h *Handler) ListBackupTargets(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	jsonResp(w, http.StatusOK, map[string]any{"targets": h.backupStore.ListTargets()})
}

func (h *Handler) CreateBackupTarget(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	var req backupTargetCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	t, err := h.backupStore.CreateTarget(req.Name, req.Path,
		backupstore.TargetType(req.Type), req.VMFilter, req.VMIDs)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.target.create", t.ID, map[string]any{"name": t.Name}))
	}
	jsonResp(w, http.StatusCreated, t)
}

func (h *Handler) UpdateBackupTarget(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	// All fields are pointers: nil = "don't change", *x = "set to x".
	// The A4 fix for bug #7: the previous code used "" for "don't
	// change" on Name/Path/Type, which made it impossible to set
	// Enabled=false explicitly — the API would silently treat it as
	// "leave alone". With pointers, false is a real value.
	var req struct {
		Name     *string             `json:"name"`
		Path     *string             `json:"path"`
		Type     *string             `json:"type"`
		VMFilter *string             `json:"vm_filter"`
		VMIDs    *[]string           `json:"vm_ids"`
		Enabled  *bool               `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	var ttype *backupstore.TargetType
	if req.Type != nil {
		tt := backupstore.TargetType(*req.Type)
		ttype = &tt
	}
	t, err := h.backupStore.UpdateTarget(id, req.Name, req.Path, ttype, req.VMFilter, req.VMIDs, req.Enabled)
	if err != nil {
		status, code := backupErrorStatus(err)
		jsonResp(w, status, map[string]any{"error": err.Error(), "code": code})
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.target.update", id, nil))
	}
	jsonResp(w, http.StatusOK, t)
}

func (h *Handler) DeleteBackupTarget(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	if err := h.backupStore.DeleteTarget(id); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.target.delete", id, nil))
	}
	jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Schedules ---

type backupScheduleCreateRequest struct {
	Name     string `json:"name"`
	Cron     string `json:"cron"`
	TargetID string `json:"target_id"`
}

func (h *Handler) ListBackupSchedules(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	jsonResp(w, http.StatusOK, map[string]any{"schedules": h.backupStore.ListSchedules()})
}

func (h *Handler) CreateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	var req backupScheduleCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	sc, err := h.backupStore.CreateSchedule(req.Name, req.Cron, req.TargetID)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.backupRunner != nil {
		h.backupRunner.Reload()
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.schedule.create", sc.ID, map[string]any{"name": sc.Name}))
	}
	jsonResp(w, http.StatusCreated, sc)
}

func (h *Handler) UpdateBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	// All fields are pointers: nil = "don't change", *x = "set to x".
	// A4 fix for bug #4: matches the new UpdateTarget convention
	// so a future "edit schedule" UI doesn't have to special-case
	// boolean/zero values.
	var req struct {
		Name     *string `json:"name"`
		Cron     *string `json:"cron"`
		TargetID *string `json:"target_id"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	sc, err := h.backupStore.UpdateSchedule(id, req.Name, req.Cron, req.TargetID, req.Enabled)
	if err != nil {
		status, code := backupErrorStatus(err)
		jsonResp(w, status, map[string]any{"error": err.Error(), "code": code})
		return
	}
	if h.backupRunner != nil {
		h.backupRunner.Reload()
	}
	jsonResp(w, http.StatusOK, sc)
}

func (h *Handler) DeleteBackupSchedule(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	if err := h.backupStore.DeleteSchedule(id); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.backupRunner != nil {
		h.backupRunner.Reload()
	}
	jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Jobs ---

func (h *Handler) ListBackupJobs(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	jsonResp(w, http.StatusOK, map[string]any{"jobs": h.backupStore.ListJobs(50)})
}

// --- Backups (files on disk) ---

func (h *Handler) ListBackupsOnTarget(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	t, ok := h.backupStore.GetTarget(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "target not found")
		return
	}
	files, err := backupstore.ListBackupsOnTarget(t)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResp(w, http.StatusOK, map[string]any{"backups": files})
}

// BackupNowRequest is the body for POST /api/backup/targets/{id}/run.
type BackupNowRequest struct{}

// BackupNow triggers a manual backup. The target may be the URL
// param id; if empty, the default target is used.
//
// The HTTP status is mapped from the runner's sentinel errors:
//   ErrTargetNotFound        → 404
//   ErrTargetDisabled        → 409
//   ErrTargetPathUnwritable  → 400
//   anything else            → 500
// The job is returned in the body on every error path so the UI
// can still render what was attempted.
func (h *Handler) BackupNow(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil || h.backupRunner == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup subsystem not initialized")
		return
	}
	id := chiURLParam(r, "id")
	if id == "" {
		id = "default"
	}
	job, err := h.backupRunner.RunOnce(r.Context(), id, "")
	if err != nil {
		status, code := backupErrorStatus(err)
		jsonResp(w, status, map[string]any{"job": job, "error": err.Error(), "code": code})
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.run", id, map[string]any{"filename": job.Filename, "size": job.Size}))
	}
	jsonResp(w, http.StatusOK, map[string]any{"job": job})
}

// backupErrorStatus turns a runner/store error into the right
// HTTP status and a stable "code" string the UI can switch on.
// Centralised so every backup endpoint (BackupNow, RestoreBackup,
// future) maps errors identically.
func backupErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, backupstore.ErrTargetNotFound):
		return http.StatusNotFound, "target_not_found"
	case errors.Is(err, backupstore.ErrTargetDisabled):
		return http.StatusConflict, "target_disabled"
	case errors.Is(err, backupstore.ErrScheduleNotFound):
		return http.StatusNotFound, "schedule_not_found"
	case errors.Is(err, backupstore.ErrInvalidCron):
		return http.StatusBadRequest, "invalid_cron"
	case errors.Is(err, backupstore.ErrTargetPathUnwritable):
		return http.StatusBadRequest, "target_path_unwritable"
	case errors.Is(err, backupstore.ErrDiskFull):
		return http.StatusInsufficientStorage, "disk_full"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

// VerifyBackup computes sha256 of a backup file.
func (h *Handler) VerifyBackup(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		jsonErr(w, http.StatusBadRequest, "filename query param required")
		return
	}
	t, ok := h.backupStore.GetTarget(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "target not found")
		return
	}
	b, err := backupstore.VerifyBackup(t, filename)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResp(w, http.StatusOK, b)
}

// RestoreBackup extracts a backup archive into a fresh directory.
// Phase II accepts two request shapes:
//
//   {"filename": "vmmanager-...-vm-1.tar.zst"}    — single file
//   {"run": "20260625T120000.000000000Z-aabbcc"} — every file in
//                                                  that backup run
//
// The handler passes the request's context so a client disconnect
// aborts the tar. Sentinel errors from the runner are mapped to
// the right HTTP status (see backupErrorStatus).
func (h *Handler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	var req struct {
		Filename string   `json:"filename"`
		Run      string   `json:"run"`
		// Files is accepted in addition to Filename/Run
		// so future clients can pass an explicit list. For
		// now, only the first entry is used; the others
		// are ignored. This is here so a Phase III UI
		// that wants a checkbox list doesn't need a
		// breaking change.
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Filename == "" && req.Run == "" && len(req.Files) == 0 {
		jsonErr(w, http.StatusBadRequest, "filename, run, or files is required")
		return
	}
	t, ok := h.backupStore.GetTarget(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "target not found")
		return
	}
	var (
		res backupstore.RestoreResult
		err error
	)
	switch {
	case req.Run != "":
		res, err = backupstore.RestoreRun(r.Context(), t, req.Run, h.cfg.DataDir, nil)
	case req.Filename != "":
		res, err = backupstore.RestoreRun(r.Context(), t, "", h.cfg.DataDir, []string{req.Filename})
	default:
		res, err = backupstore.RestoreRun(r.Context(), t, "", h.cfg.DataDir, req.Files)
	}
	if err != nil {
		status, code := backupErrorStatus(err)
		jsonResp(w, status, map[string]any{"error": err.Error(), "code": code})
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.restore", id, map[string]any{
			"to":         res.Destination,
			"file_count": len(res.Files),
		}))
	}
	jsonResp(w, http.StatusOK, res)
}

// RestoreAsVM is the operator-friendly restore: it takes a backup
// archive already on the target's directory and creates a new VM
// in libvirt from it — exactly what POST /api/vms/import does for
// a freshly uploaded archive, but without the re-upload round-trip.
//
// Request body:
//
//	{
//	  "filename": "vmmanager-host-20260626T152618-...-7be64cc4-....tar.zst",
//	  "name":     "ubuntu-1-restored",   // optional; default derived
//	  "pool":     "vmmanager-disks"          // optional; default config.DiskPoolName
//	}
//
// The new VM is registered in libvirt with the domain XML and
// disk from the archive. The response includes the new VM's uuid
// and resolved name. The source archive is NOT deleted; the
// operator can keep it as an additional safety copy or remove it
// from the Files tab when ready.
//
// This handler is the fix for the "Restore button just extracts
// to a dir, doesn't actually restore" UX bug surfaced in the v4
// release: the old POST /restore endpoint kept its extract-only
// semantics (moved to the legacy code path) and a new endpoint
// with import-like semantics was added.
func (h *Handler) RestoreAsVM(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	if h.lv == nil {
		jsonErr(w, http.StatusServiceUnavailable, "libvirt connector not initialized")
		return
	}
	id := chiURLParam(r, "id")
	var req struct {
		Filename string `json:"filename"`
		Name     string `json:"name"`
		Pool     string `json:"pool"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Filename == "" {
		jsonErr(w, http.StatusBadRequest, "filename is required")
		return
	}
	if req.Pool == "" {
		req.Pool = config.DiskPoolName
	}
	t, ok := h.backupStore.GetTarget(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "target not found")
		return
	}
	// ValidBackupFilename also enforces the regex shape, so a
	// caller can't pass "../../etc/passwd" and have us open
	// something outside the target's directory.
	if !backupstore.ValidBackupFilename(req.Filename) {
		jsonErr(w, http.StatusBadRequest, "invalid filename format")
		return
	}
	sourcePath := filepath.Join(t.Path, req.Filename)
	fi, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonErr(w, http.StatusNotFound, "backup file not found on target")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "stat backup: "+err.Error())
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "vm.restore", "pending", map[string]any{
			"action":   "start",
			"target":   id,
			"filename": req.Filename,
			"size":     fi.Size(),
			"pool":     req.Pool,
			"name":     req.Name,
		}))
	}
	uuid, resolvedName, warnings, format, err := h.importLocalArchive(
		sourcePath, req.Filename, fi.Size(),
		strings.TrimSpace(req.Name), req.Pool, false,
	)
	if err != nil {
		if h.audit != nil {
			h.audit.Log(auditFor(r, "vm.restore_failed", "unknown", map[string]any{
				"target":   id,
				"filename": req.Filename,
				"error":    err.Error(),
			}))
		}
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "vm.restore", uuid, map[string]any{
			"action":   "done",
			"target":   id,
			"filename": req.Filename,
			"name":     resolvedName,
			"size":     fi.Size(),
			"warnings": len(warnings),
		}))
	}
	resp := map[string]any{
		"status":         "restored",
		"id":             uuid,
		"name":           resolvedName,
		"requested_name": req.Name,
		"filename":       req.Filename,
		"format":         format,
		"source":         "backup-target",
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	jsonResp(w, http.StatusCreated, resp)
}

// DeleteBackupFile removes one archive from a target's path. This
// is the only way an operator can clean up disk usage now: there
// is no retention policy, no auto-prune, no scheduled cleanup.
// The filename is matched against the runner's strict
// vmmanager-<host>-<UTC>.tar.gz pattern server-side so a request for
// "../../etc/passwd" gets a 400, not a surprise deletion.
func (h *Handler) DeleteBackupFile(w http.ResponseWriter, r *http.Request) {
	if h.backupStore == nil {
		jsonErr(w, http.StatusServiceUnavailable, "backup store not initialized")
		return
	}
	id := chiURLParam(r, "id")
	filename := chiURLParam(r, "filename")
	if filename == "" {
		jsonErr(w, http.StatusBadRequest, "filename is required")
		return
	}
	t, ok := h.backupStore.GetTarget(id)
	if !ok {
		jsonErr(w, http.StatusNotFound, "target not found")
		return
	}
	if err := backupstore.DeleteBackupFile(t, filename); err != nil {
		// 404 for missing file is more useful to the UI than 500:
		// the file may have been deleted from another tab, and
		// the user just wants a quiet refresh.
		if os.IsNotExist(err) {
			jsonErr(w, http.StatusNotFound, "file not found")
			return
		}
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.audit != nil {
		h.audit.Log(auditFor(r, "backup.file.delete", id, map[string]any{"filename": filename}))
	}
	jsonResp(w, http.StatusOK, map[string]bool{"ok": true})
}
