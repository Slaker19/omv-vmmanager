package libvirt

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/libvirt/libvirt-go"
	"omv-vmmanager/internal/config"
)

type Connector struct {
	uri      string
	cfg      *config.Config
	conn     *libvirt.Connect
	mu       sync.RWMutex
	purposes *PoolPurposeStore
}

func NewConnector(uri string, cfg *config.Config) *Connector {
	c := &Connector{uri: uri, cfg: cfg}
	if cfg != nil {
		c.purposes = NewPoolPurposeStore(cfg.DataDir)
	}
	return c
}

// IsConnected reports whether the libvirt connection is currently alive.
// Safe to call from any goroutine.
func (c *Connector) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil {
		return false
	}
	alive, err := c.conn.IsAlive()
	return err == nil && alive
}

func (c *Connector) Open() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := libvirt.NewConnect(c.uri)
	if err != nil {
		return fmt.Errorf("libvirt connect: %w", err)
	}
	if alive, err := conn.IsAlive(); err != nil || !alive {
		if conn != nil {
			conn.Close()
		}
		return fmt.Errorf("libvirt connection not alive: %w", err)
	}
	c.conn = conn
	return nil
}

func (c *Connector) DiskPoolName() string {
	return config.DiskPoolName
}

func (c *Connector) ISOPoolName() string {
	return config.ISOPoolName
}

func (c *Connector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Connector) Get() *libvirt.Connect {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// EnsureConnected re-establishes the libvirt connection if needed.
// Exported alias for the private ensureConnected used by callers in
// other packages (api, events) that need to touch the connection but
// not hold the lock.
func (c *Connector) EnsureConnected() error {
	return c.ensureConnected()
}

func (c *Connector) EnsureDefaults() {
	if err := c.ensureConnected(); err != nil {
		// libvirtd is unreachable at boot. The default pools
		// (vmmanager-disks, ISOS) won't be created this time around;
		// the next start (or a manual libvirtd restart) will retry.
		// Log a warning so the operator doesn't spend an hour
		// hunting a "missing pool" bug that's really a libvirtd
		// connectivity issue.
		slog.Warn("ensure_defaults_skipped_libvirt_unreachable",
			"err", err.Error())
		return
	}

	if c.cfg == nil {
		slog.Warn("no_config_skipping_default_pools")
		return
	}

	if c.purposes != nil {
		if err := c.purposes.Load(); err != nil {
			slog.Warn("pool_purposes_load_failed", "err", err)
		}
	}

	if err := os.MkdirAll(c.cfg.DataDir, 0755); err != nil {
		slog.Warn("data_dir_create_failed", "path", c.cfg.DataDir, "err", err)
		return
	}
	if err := os.MkdirAll(c.cfg.PoolsDir(), 0755); err != nil {
		slog.Warn("pools_dir_create_failed", "path", c.cfg.PoolsDir(), "err", err)
		return
	}

	c.ensureDefaultNetwork()
	c.ensureDefaultBridgeNetwork()
	c.ensureAppPool(config.DiskPoolName, c.cfg.DiskPoolPath())
	c.ensureAppPool(config.ISOPoolName, c.cfg.ISOPoolPath())
	c.setDefaultPoolPurposes()
	c.WarnUnreadableDisks()
}

func (c *Connector) setDefaultPoolPurposes() {
	if c.purposes == nil {
		return
	}
	if c.purposes.Get(config.DiskPoolName) == "" {
		if err := c.purposes.Set(config.DiskPoolName, PoolPurposeDisk); err != nil {
			slog.Warn("pool_purpose_save_failed", "pool", config.DiskPoolName, "err", err)
		}
	}
	if c.purposes.Get(config.ISOPoolName) == "" {
		if err := c.purposes.Set(config.ISOPoolName, PoolPurposeISO); err != nil {
			slog.Warn("pool_purpose_save_failed", "pool", config.ISOPoolName, "err", err)
		}
	}
}

func (c *Connector) ensureDefaultNetwork() {
	net, err := c.conn.LookupNetworkByName("default")
	if err != nil {
		xmlStr := `<network>
  <name>default</name>
  <forward mode='nat'/>
  <bridge name='virbr0' stp='on' delay='0'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>`
		n, err := c.conn.NetworkDefineXML(xmlStr)
		if err != nil {
			slog.Warn("default_network_define_failed", "err", err)
			return
		}
		defer n.Free()
		if err := n.SetAutostart(true); err != nil {
			slog.Warn("default_network_autostart_failed", "err", err)
		}
		if err := n.Create(); err != nil {
			slog.Warn("default_network_start_failed", "err", err)
			return
		}
		slog.Info("default_network_created")
		return
	}
	defer net.Free()

	active, _ := net.IsActive()
	if !active {
		if err := net.Create(); err != nil {
			slog.Warn("default_network_start_failed", "err", err)
			return
		}
		slog.Info("default_network_started")
	}
}

func (c *Connector) ensureAppPool(name, path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		slog.Warn("pool_dir_create_failed", "path", path, "err", err)
		return
	}

	cleanPath := filepath.Clean(path)

	pool, err := c.conn.LookupStoragePoolByName(name)
	if err != nil {
		c.defineAndStartAppPool(name, cleanPath)
		return
	}
	defer pool.Free()

	// If the pool already exists but points to a different path (e.g. after
	// changing DATA_DIR), undefine it and recreate it at the new location.
	xmlDesc, _ := pool.GetXMLDesc(0)
	currentPath := extractPoolPath(xmlDesc)
	if currentPath != "" && filepath.Clean(currentPath) != cleanPath {
		slog.Info("pool_path_changed_recreating", "pool", name, "old_path", currentPath, "new_path", cleanPath)
		info, _ := pool.GetInfo()
		if info.State == libvirt.STORAGE_POOL_RUNNING {
			if err := pool.Destroy(); err != nil {
				slog.Warn("pool_stop_failed", "pool", name, "err", err)
				return
			}
		}
		if err := pool.Undefine(); err != nil {
			slog.Warn("pool_undefine_failed", "pool", name, "err", err)
			return
		}
		c.defineAndStartAppPool(name, cleanPath)
		return
	}

	info, _ := pool.GetInfo()
	if info.State != libvirt.STORAGE_POOL_RUNNING {
		if err := pool.Create(0); err != nil {
			slog.Warn("pool_start_failed", "pool", name, "err", err)
			return
		}
		slog.Info("pool_started", "pool", name)
	}
}

func (c *Connector) defineAndStartAppPool(name, cleanPath string) {
	xmlStr := fmt.Sprintf(`<pool type='dir'>
  <name>%s</name>
  <target>
    <path>%s</path>
    <permissions>
      <mode>0755</mode>
    </permissions>
  </target>
</pool>`, name, cleanPath)
	p, err := c.conn.StoragePoolDefineXML(xmlStr, 0)
	if err != nil {
		slog.Warn("pool_define_failed", "pool", name, "err", err)
		return
	}
	defer p.Free()
	if err := p.Build(0); err != nil {
		slog.Warn("pool_build_failed", "pool", name, "err", err)
		return
	}
	if err := p.SetAutostart(true); err != nil {
		slog.Warn("pool_autostart_failed", "pool", name, "err", err)
	}
	if err := p.Create(0); err != nil {
		slog.Warn("pool_start_failed", "pool", name, "err", err)
		return
	}
	slog.Info("pool_created", "pool", name, "path", cleanPath)
	if c.purposes != nil && c.purposes.Get(name) == "" {
		purpose := InferPoolPurpose(name)
		if err := c.purposes.Set(name, purpose); err != nil {
			slog.Warn("pool_purpose_save_failed", "pool", name, "err", err)
		}
	}
}

func (c *Connector) ensureDefaultBridgeNetwork() {
	const name = "vmmanager-bridge"
	net, err := c.conn.LookupNetworkByName(name)
	if err == nil {
		net.Free()
		return
	}
	iface, err := defaultRouteInterface()
	if err != nil {
		slog.Warn("skip_vmmanager_bridge_network", "reason", "no_default_route", "err", err)
		return
	}
	xmlStr := fmt.Sprintf(`<network>
  <name>%s</name>
  <forward mode='bridge'>
    <interface dev='%s'/>
  </forward>
</network>`, name, iface)
	n, err := c.conn.NetworkDefineXML(xmlStr)
	if err != nil {
		slog.Warn("vmmanager_bridge_define_failed", "err", err)
		return
	}
	defer n.Free()
	if err := n.SetAutostart(true); err != nil {
		slog.Warn("vmmanager_bridge_autostart_failed", "err", err)
	}
	if err := n.Create(); err != nil {
		slog.Warn("vmmanager_bridge_start_failed", "err", err)
		return
	}
	slog.Info("vmmanager_bridge_network_created", "iface", iface, "name", name)
}

func defaultRouteInterface() (string, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", fmt.Errorf("read /proc/net/route: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "00000000" {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no default route found in /proc/net/route")
}

func (c *Connector) ensureConnected() error {
	if c.conn == nil {
		return fmt.Errorf("not connected to libvirt")
	}
	alive, err := c.conn.IsAlive()
	if err != nil || !alive {
		return fmt.Errorf("libvirt connection lost")
	}
	return nil
}
