package models

type VMState string

const (
	VMStateRunning  VMState = "running"
	VMStateShutoff  VMState = "shutoff"
	VMStatePaused   VMState = "paused"
	VMStateCrashed  VMState = "crashed"
	VMStateUnknown  VMState = "unknown"
)

type VM struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	State      VMState  `json:"state"`
	VCPUs      int      `json:"vcpus"`
	RAMMB      int64    `json:"ram_mb"`
	DiskGB     int64    `json:"disk_gb"`
	OSIcon     string   `json:"os_icon,omitempty"`
	UptimeSec  int64    `json:"uptime_sec,omitempty"`
	CPUUsage   float64  `json:"cpu_usage,omitempty"`
	RAMUsedMB  int64    `json:"ram_used_mb,omitempty"`
	OSType     string   `json:"os_type,omitempty"`
	OSVersion  string   `json:"os_version,omitempty"`
	Chipset    string   `json:"chipset,omitempty"`
	SecureBoot bool     `json:"secure_boot"`
	TPMEnabled bool     `json:"tpm_enabled"`
	// Autostart mirrors libvirtd's autostart flag: when true,
	// libvirtd starts the domain automatically on host boot.
	// Surfaced here so the UI doesn't need a second round-trip
	// to GET /vms/{id}/autostart after fetching the VM.
	Autostart  bool     `json:"autostart"`
	Firmware   string   `json:"firmware,omitempty"`
	CPUMode    string   `json:"cpu_mode,omitempty"`
	VideoModel string   `json:"video_model,omitempty"`
	IP         string   `json:"ip,omitempty"`
	Alias      string   `json:"alias,omitempty"`
	Cover      string   `json:"cover,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Disks      []DiskInfo  `json:"disks,omitempty"`
	Networks   []NetIface  `json:"networks,omitempty"`
}

type CreateVMRequest struct {
	Name             string `json:"name"`
	VCPUs            int    `json:"vcpus"`
	RAMMB            int64  `json:"ram_mb"`
	DiskGB           int64  `json:"disk_gb"`
	ISO              string `json:"iso,omitempty"`
	Network          string `json:"network,omitempty"`
	StoragePool      string `json:"storage_pool,omitempty"`
	OSVariant        string `json:"os_variant,omitempty"`
	CPUMode          string `json:"cpu_mode,omitempty"`
	CPUModel         string `json:"cpu_model,omitempty"`
	VideoModel       string `json:"video_model,omitempty"`
	NetworkModel     string `json:"network_model,omitempty"`
	DiskBus          string `json:"disk_bus,omitempty"`
	OSType           string `json:"os_type,omitempty"`
	OSVersion        string `json:"os_version,omitempty"`
	Chipset          string `json:"chipset,omitempty"`
	SecureBoot       *bool  `json:"secure_boot,omitempty"`
	TPMEnabled       *bool  `json:"tpm_enabled,omitempty"`
	Firmware         string `json:"firmware,omitempty"`
	DiskFormat       string `json:"disk_format,omitempty"`
	VirtIOISO        string `json:"virtio_iso,omitempty"`
	ExistingDiskPool string `json:"existing_disk_pool,omitempty"`
	ExistingDiskName string `json:"existing_disk_name,omitempty"`
}

type UpdateVMRequest struct {
	Name        *string `json:"name,omitempty"`
	VCPUs       *int    `json:"vcpus,omitempty"`
	RAMMB       *int64  `json:"ram_mb,omitempty"`
	DiskGB      *int64  `json:"disk_gb,omitempty"`
	CPUMode     *string `json:"cpu_mode,omitempty"`
	VideoModel  *string `json:"video_model,omitempty"`
	Network     *string `json:"network,omitempty"`
	NetworkModel *string `json:"network_model,omitempty"`
	OSType      *string `json:"os_type,omitempty"`
	OSVersion   *string `json:"os_version,omitempty"`
	Chipset     *string `json:"chipset,omitempty"`
	SecureBoot  *bool   `json:"secure_boot,omitempty"`
	TPMEnabled  *bool   `json:"tpm_enabled,omitempty"`
	Firmware    *string `json:"firmware,omitempty"`
}

type DiskInfo struct {
	Device   string `json:"device"` // disk, cdrom
	Bus      string `json:"bus"`    // virtio, sata, scsi, ide
	Target   string `json:"target"` // vda, sda, hda, etc.
	Source   string `json:"source"` // file path
	Name     string `json:"name"`   // display name (root backing file basename)
	Pool     string `json:"pool,omitempty"`
	SizeGB   int64  `json:"size_gb,omitempty"`
	ReadOnly bool   `json:"readonly"`
	Type     string `json:"type"` // file, block
}

type AttachDiskRequest struct {
	Device string `json:"device"` // disk, cdrom
	Bus    string `json:"bus"`    // virtio, sata, scsi, ide
	Source string `json:"source,omitempty"` // for cdrom: ISO path
	SizeGB int64  `json:"size_gb,omitempty"` // for disk: new size
	Pool   string `json:"pool,omitempty"` // storage pool for new disk
	Format string `json:"format,omitempty"` // for disk: qcow2, raw
}

type NetIface struct {
	MAC      string `json:"mac"`
	Network  string `json:"network"`
	Model    string `json:"model"`
	Type     string `json:"type"` // network, bridge
	Source   string `json:"source,omitempty"`
}

type AttachNetRequest struct {
	Network string `json:"network"`
	Model   string `json:"model,omitempty"`
}

type CloneVMRequest struct {
	Name     string `json:"name"`
	Pool     string `json:"pool,omitempty"`
}

type Snapshot struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	CreatedAt       string `json:"created_at"`    // legacy RFC3339; kept for compat
	CreationTime    int64  `json:"creation_time"` // epoch seconds, used by the tree UI
	ParentName      string `json:"parent_name"`   // empty = root snapshot
	Current         bool   `json:"current"`
	SizeAtSnapBytes int64  `json:"size_at_snap_bytes,omitempty"` // libvirt "Allocated" at creation
}

type CreateSnapshotRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type StoragePool struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Path       string `json:"path"`
	Purpose    string `json:"purpose"`
	Capacity   int64  `json:"capacity"`
	Allocated  int64  `json:"allocated"`
	Available  int64  `json:"available"`
	State      string `json:"state"`
	Autostart  bool   `json:"autostart"`
}

type CreatePoolRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`        // dir, netfs
	Path        string `json:"path"`        // local target path (where the mount lands)
	SourceHost  string `json:"source_host"` // for netfs
	SourceDir   string `json:"source_dir"`  // for netfs
	SourceFormat string `json:"source_format"` // nfs, cifs (defaults to nfs)
	Purpose     string `json:"purpose"`

	// SourceUsername / SourcePassword are accepted for netfs pools
	// with SourceFormat=cifs. Both must be set together (validated
	// in the API handler with HTTP 400) or the backend refuses to
	// build the <auth> block.
	SourceUsername string `json:"source_username,omitempty"`
	SourcePassword string `json:"source_password,omitempty"`

	// SecretUUID is set internally by the libvirt layer after a
	// successful defineCIFSSecret. It is never accepted from the
	// client and never echoed back. Hidden from JSON with `json:"-"`.
	SecretUUID string `json:"-"`
}

// UpdatePoolRequest is the body of PUT /api/storage/pools/{name}.
// All fields are optional; nil pointer means "leave unchanged".
// Pointer-based booleans are not used here because the only
// "toggle" we expose today is CifsNeedsReauth, which is itself
// a request-time signal (not a stored state) and is therefore
// represented as a plain bool with omitempty.
type UpdatePoolRequest struct {
	Path         *string `json:"path,omitempty"`
	SourceHost   *string `json:"source_host,omitempty"`
	SourceDir    *string `json:"source_dir,omitempty"`
	SourceFormat *string `json:"source_format,omitempty"`

	// CIFS auth fields. Must come together (both nil or both set)
	// to avoid silently misconfiguring the pool. The handler
	// rejects mismatched values with HTTP 400.
	SourceUsername *string `json:"source_username,omitempty"`
	SourcePassword *string `json:"source_password,omitempty"`

	// CifsNeedsReauth: when true, the backend re-creates the
	// libvirt secret for this pool using the supplied credentials
	// and updates the pool XML to reference the new secret UUID.
	// Use this after a libvirtd reinstall, or to rotate the
	// password without recreating the pool. Requires operator
	// role.
	CifsNeedsReauth bool `json:"cifs-needs-reauth,omitempty"`
}

type StorageVolume struct {
	Name      string `json:"name"`
	Pool      string `json:"pool"`
	Path      string `json:"path"`
	Format    string `json:"format"`
	Capacity  int64  `json:"capacity"`
	Allocated int64  `json:"allocated"`
	// IsSnapshot is true when this volume is an internal qcow2 snapshot
	// view (e.g. "vm1.snap1") rather than a real file on disk. Resize
	// and delete are not supported for snapshots.
	IsSnapshot bool `json:"is_snapshot,omitempty"`
	// SnapshotOfVMID is the UUID of the VM that owns this snapshot.
	// Empty when IsSnapshot is false.
	SnapshotOfVMID string `json:"snapshot_of_vm_id,omitempty"`
	// ParentVolume is the name of the root disk (e.g. "vm1.qcow2")
	// that this snapshot belongs to. Empty when IsSnapshot is false.
	ParentVolume string `json:"parent_volume,omitempty"`
}

type CreateVolumeRequest struct {
	Name     string `json:"name"`
	Pool     string `json:"pool"`
	Capacity int64  `json:"capacity"`
	Format   string `json:"format"`
}

type Network struct {
	Name        string   `json:"name"`
	Forward     string   `json:"forward"`
	Bridge      string   `json:"bridge"`
	CIDR        string   `json:"cidr"`
	DHCP        bool     `json:"dhcp"`
	DHCPStart   string   `json:"dhcp_start,omitempty"`
	DHCPEnd     string   `json:"dhcp_end,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	DNS         []string `json:"dns,omitempty"` // DNS forwarders for dnsmasq
	Active      bool     `json:"active"`
	Autostart   bool     `json:"autostart"`
	// Protected is true for networks that webVM's setup-bridge.sh
	// auto-creates. The API refuses to delete these (and the UI
	// greys out the delete button) so a stray click can't silently
	// remove the bridge that holds the host's LAN IP.
	Protected bool `json:"protected,omitempty"`
}

type CreateNetworkRequest struct {
	Name        string   `json:"name"`
	Forward     string   `json:"forward"`
	CIDR        string   `json:"cidr"`
	Bridge      string   `json:"bridge,omitempty"`
	DHCP        *bool    `json:"dhcp,omitempty"`
	DHCPStart   string   `json:"dhcp_start,omitempty"`
	DHCPEnd     string   `json:"dhcp_end,omitempty"`
	DNS         []string `json:"dns,omitempty"`
	Autostart   *bool    `json:"autostart,omitempty"`
}

type UpdateNetworkRequest struct {
	DHCP        *bool    `json:"dhcp,omitempty"`
	DHCPStart   string   `json:"dhcp_start,omitempty"`
	DHCPEnd     string   `json:"dhcp_end,omitempty"`
	DNS         []string `json:"dns,omitempty"`
	Autostart   *bool    `json:"autostart,omitempty"`
}

type HostInfo struct {
	Hostname      string `json:"hostname"`
	Architecture  string `json:"architecture"`
	CPUCores      int    `json:"cpu_cores"`
	CPUThreads    int    `json:"cpu_threads"`
	CPUSockets    int    `json:"cpu_sockets"`
	CPUModel      string `json:"cpu_model"`
	TotalRAM      int64  `json:"total_ram"`
	FreeRAM       int64  `json:"free_ram"`
	LibvirtVersion string `json:"libvirt_version"`
	QEMUVersion   string `json:"qemu_version"`
}

type HostStats struct {
	CPUUsage  float64 `json:"cpu_usage"`
	UsedRAM   int64   `json:"used_ram"`
	TotalRAM  int64   `json:"total_ram"`
	UsedDisk  int64   `json:"used_disk"`
	TotalDisk int64   `json:"total_disk"`
}

// HostInterface represents a physical/logical host network interface that
// can be used as a bridge target for libvirt bridge-mode networks.
type HostInterface struct {
	Name     string `json:"name"`
	Type     string `json:"type"`      // "ethernet", "wifi", "bond", "vlan"
	State    string `json:"state"`     // "up", "down", "unknown"
	MAC      string `json:"mac"`
	IPSource string `json:"ip_source"` // "static" | "dhcp" | "none"
}

// VMMeta is the omv-vmmanager app-level metadata stored inside a
// libvirt domain's <metadata><vmmanager:meta> element. Persists with
// the domain via libvirt; no separate database needed.
type VMMeta struct {
	Alias    string   `xml:"alias"     json:"alias,omitempty"`
	Notes    string   `xml:"notes"     json:"notes,omitempty"`
	Cover    string   `xml:"cover"     json:"cover,omitempty"`
	Groups   []string `xml:"groups>group,omitempty" json:"groups,omitempty"`
	UpdatedAt int64   `xml:"updated_at" json:"updated_at,omitempty"`
}

// VMMetaUpdate carries partial updates from the API; nil pointers mean
// "leave unchanged". Use slice* helpers in metadata.go to apply these.
type VMMetaUpdate struct {
	Alias  *string   `json:"alias,omitempty"`
	Notes  *string   `json:"notes,omitempty"`
	Cover  *string   `json:"cover,omitempty"`
	Groups *[]string `json:"groups,omitempty"`
}

// CoverUploadResponse is the response of POST /api/vms/{id}/cover.
type CoverUploadResponse struct {
	URL    string `json:"url"`
	Path   string `json:"path"`
	Format string `json:"format"`
}

// VlanSupport describes whether VLAN tagging is supported for a given
// network on this host.
type VlanSupport struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// UpdateNetIfaceRequest is the payload of PATCH /api/vms/{id}/networks/{mac}.
type UpdateNetIfaceRequest struct {
	MAC     *string `json:"mac,omitempty"`
	Network *string `json:"network,omitempty"`
	VLANTag *int    `json:"vlan_tag,omitempty"` // nil = leave, 0 = remove VLAN
}

// Group is a tag/label with a color, shared across VMs. Stored in
// /var/lib/vmmanager/groups.json (one definition per app, not per VM).
type Group struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	MemberCount int    `json:"member_count"`
}

// GroupList is the response of GET /api/groups.
type GroupList struct {
	Groups []Group `json:"groups"`
}

// GroupUpsertRequest is the body of POST /api/groups and PUT /api/groups/{name}.
type GroupUpsertRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// MetricsSample is a single timestamped datapoint for a metric.
type MetricsSample struct {
	T int64   `json:"t"` // epoch seconds
	V float64 `json:"v"`
}

// MetricsSeries is a time series of samples.
type MetricsSeries struct {
	Kind string          `json:"kind"` // "cpu" | "ram" | "disk_r" | "disk_w" | "net_rx" | "net_tx"
	Unit  string         `json:"unit"`
	Window int            `json:"window"` // seconds
	Points []MetricsSample `json:"points"`
}

// VMMetrics bundles all the metric series for a single VM. Returned by
// GET /api/vms/{id}/metrics and pushed via the "vm.metrics" SSE event.
type VMMetrics struct {
	VMID    string         `json:"vm_id"`
	SampledAt int64        `json:"sampled_at"`
	CPU     MetricsSeries  `json:"cpu"`
	RAM     MetricsSeries  `json:"ram"`
	DiskRead  MetricsSeries `json:"disk_read"`
	DiskWrite MetricsSeries `json:"disk_write"`
	NetRx     MetricsSeries `json:"net_rx"`
	NetTx     MetricsSeries `json:"net_tx"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token               string `json:"token"`
	ExpiresAt           int64  `json:"expires_at"`
	Username            string `json:"username"`
	Role                string `json:"role"`
	MustChangePassword  bool   `json:"must_change_password"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type ISOScanResult struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Pool string `json:"pool"`
}

// Role constants for the fixed 3-tier RBAC model.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// IsValidRole returns true if r is one of the recognised fixed roles.
func IsValidRole(r string) bool {
	return r == RoleAdmin || r == RoleOperator || r == RoleViewer
}

// User is the on-disk user record. The bcrypt hash IS persisted to
// disk (so the password survives restarts) under the field name
// "password_hash". The ToResponse() projection is used in API
// responses to keep the hash out of HTTP traffic.
//
// Note: this struct is also returned by the on-disk
// (de)serializer in `internal/user/store.go`, which uses
// json:"-" for `PasswordHash` via a shadow type to prevent the
// hash from being leaked in the in-memory API surface. See
// `userStoreFile` and the `(u) MarshalJSON` / `UnmarshalJSON`
// methods below.
type User struct {
	Username             string `json:"username"`
	PasswordHash         string `json:"password_hash,omitempty"`
	Role                 string `json:"role"`
	CreatedAt            string `json:"created_at"`
	Email                string `json:"email,omitempty"`
	Active               bool   `json:"active"`
	MustChangePassword   bool   `json:"must_change_password"`
	LastLoginAt          string `json:"last_login_at,omitempty"`
}

// UserResponse is the API-facing projection of User; it is what gets
// returned by /api/users endpoints and /api/auth/me. The hash is
// deliberately excluded.
type UserResponse struct {
	Username           string `json:"username"`
	Role               string `json:"role"`
	CreatedAt          string `json:"created_at"`
	Email              string `json:"email,omitempty"`
	Active             bool   `json:"active"`
	MustChangePassword bool   `json:"must_change_password"`
	LastLoginAt        string `json:"last_login_at,omitempty"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		Username:           u.Username,
		Role:               u.Role,
		CreatedAt:          u.CreatedAt,
		Email:              u.Email,
		Active:             u.Active,
		MustChangePassword: u.MustChangePassword,
		LastLoginAt:        u.LastLoginAt,
	}
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Email    string `json:"email,omitempty"`
}

type UpdateUserRequest struct {
	Password *string `json:"password,omitempty"`
	Role     *string `json:"role,omitempty"`
	Email    *string `json:"email,omitempty"`
	Active   *bool   `json:"active,omitempty"`
}

// ChangeMyPasswordRequest is the body of PUT /api/users/me/password.
type ChangeMyPasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type DownloadISORequest struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
	Pool string `json:"pool,omitempty"`
}

// DownloadJob tracks progress of a background ISO download

type DownloadJob struct {
	ID       string  `json:"id"`
	Name     string  `json:"name,omitempty"`
	URL      string  `json:"url,omitempty"`
	Progress float64 `json:"progress"`
	Status   string  `json:"status"`
	Error    string  `json:"error,omitempty"`
}
