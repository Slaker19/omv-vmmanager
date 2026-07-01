<?php
require_once('openmediavault/functions.inc');

/**
 * RPC service for omv-vmmanager — bridges the OMV UI to the Go backend.
 */
class OMVRpcServiceVmManager extends \OMV\Rpc\ServiceAbstract
{
    public function getName()
    {
        return 'VmManager';
    }

    public function initialize()
    {
        $this->registerMethod('getSettings');
        $this->registerMethod('setSettings');
        $this->registerMethod('getStatus');
        $this->registerMethod('getServiceStatus');
        $this->registerMethod('getUIURL');
        $this->registerMethod('restartService');
        $this->registerMethod('getLogs');
        $this->registerMethod('getHostStats');
        $this->registerMethod('listVms');
        $this->registerMethod('getNetworkXml');
        $this->registerMethod('putNetworkXml');
        $this->registerMethod('getVmList');
        $this->registerMethod('vmCommand');
        $this->registerMethod('listStoragePools');
        $this->registerMethod('listNetworks');

        // Network management
        $this->registerMethod('getNetworkList');
        $this->registerMethod('getNetwork');
        $this->registerMethod('setNetwork');
        $this->registerMethod('updateNetwork');
        $this->registerMethod('setBridgeNetwork');
        $this->registerMethod('setMacvtap');
        $this->registerMethod('networkCommand');
        $this->registerMethod('enumerateBridges');
        $this->registerMethod('getNetworkXml');

        // Pool management
        $this->registerMethod('getPoolList');
        $this->registerMethod('createPool');
        $this->registerMethod('updatePool');
        $this->registerMethod('deletePool');
        $this->registerMethod('poolCommand');
    }

    // -----------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------

    private function backendURL($path = '')
    {
        $cfg   = \OMV\Config\Database::getInstance()->get('conf.service.vmmanager');
        $bind  = $cfg->get('bind', '127.0.0.1');
        $port  = (int)$cfg->get('port', 8080);
        return sprintf('http://%s:%d%s', $bind, $port, $path);
    }

    private function getAPIKey()
    {
        $kf = '/etc/openmediavault-vmmanager/apikey.key';
        if (!is_readable($kf)) {
            throw new \OMV\Rpc\Exception(
                'omv-vmmanager API key not provisioned.'
            );
        }
        return trim(file_get_contents($kf));
    }

    private function backendGET($path)
    {
        $ch = curl_init($this->backendURL($path));
        curl_setopt_array($ch, [
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER     => [
                'Authorization: Bearer ' . $this->getAPIKey(),
                'Accept: application/json',
            ],
            CURLOPT_TIMEOUT => 8,
        ]);
        $body = curl_exec($ch);
        $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $err  = curl_error($ch);
        curl_close($ch);
        if ($code === 0) {
            throw new \OMV\Rpc\Exception('Backend unreachable: ' . $err);
        }
        if ($code >= 400) {
            $detail = '';
            if (is_string($body) && $body !== '') {
                $decoded = json_decode($body, true);
                if (is_array($decoded) && isset($decoded['error'])) {
                    $detail = ': ' . $decoded['error'];
                }
            }
            throw new \OMV\Rpc\Exception(sprintf(
                'Backend HTTP %d%s', $code, $detail
            ));
        }
        return json_decode($body, true) ?? [];
    }

    // -----------------------------------------------------------------
    // OMV-side methods
    // -----------------------------------------------------------------

    public function getSettings($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        $db = \OMV\Config\Database::getInstance();
        $object = $db->get('conf.service.vmmanager');
        $data = $object->getAssoc();
        $data['enable']            = (bool)$data['enable'];
        $data['httpsPort']         = (int)$data['httpsPort'];
        $data['port']              = (int)$data['port'];
        $data['sslcertificateref'] = $data['sslcertificateref'] ?? '';
        return $data;
    }

    public function setSettings($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $db = \OMV\Config\Database::getInstance();
        $object = $db->get('conf.service.vmmanager');
        // Trim all string inputs to match the KVM plugin style
        // (avoids accidental leading/trailing whitespace from form
        // submission that would otherwise be saved to confdb).
        foreach (['httpsMode','sslcertificateref','dataDir','bind'] as $k) {
            if (array_key_exists($k, $params) && is_string($params[$k])) {
                $params[$k] = trim($params[$k]);
            }
        }
        // Sanitize and validate dataDir for path traversal.
        if (array_key_exists('dataDir', $params)) {
            $dir = rtrim($params['dataDir'], '/');
            if ($dir === '' || $dir === '/') {
                $dir = '/opt/openmediavault/vmmanager';
            }
            // Collapse repeated slashes the way KVM does
            // (KVM: str_replace('//', '/', $path)) so a user typing
            // /opt//openmediavault/vmmanager gets the same dir as
            // /opt/openmediavault/vmmanager.
            $dir = str_replace('//', '/', $dir);
            if (strlen($dir) > 256 || !preg_match('#^/[a-zA-Z0-9/._-]+$#', $dir)) {
                throw new \OMV\Rpc\Exception(
                    'Invalid data directory: must be an absolute path with valid characters.'
                );
            }
            $blocked = ['/etc', '/usr', '/bin', '/sbin', '/lib', '/lib64', '/dev', '/proc', '/sys', '/boot', '/root', '/var/lib/dpkg', '/var/lib/rpm', '/var/lib/libvirt'];
            foreach ($blocked as $b) {
                if ($dir === $b || strpos($dir, $b . '/') === 0) {
                    throw new \OMV\Rpc\Exception(
                        'Data directory must not be a system directory.'
                    );
                }
            }
            $params['dataDir'] = $dir;
        }
        // Validate httpsPort: must be >= 1024 (non-privileged).
        if (array_key_exists('httpsPort', $params)) {
            $port = (int)$params['httpsPort'];
            if ($port < 1024 || $port > 65535) {
                throw new \OMV\Rpc\Exception(
                    'HTTPS port must be in range 1024-65535 (non-privileged).'
                );
            }
        }
        foreach (['enable','httpsMode','httpsPort','sslcertificateref',
                  'dataDir','port','bind'] as $k) {
            if (array_key_exists($k, $params)) {
                $object->set($k, $params[$k]);
            }
        }
        $db->set($object);
        $dispatcher = \OMV\Engine\Notify\Dispatcher::getInstance();
        $dispatcher->notify(OMV_NOTIFY_MODIFY,
            'org.openmediavault.conf.service.vmmanager', $object);

        // Update the Go backend's config.json with the backend port and bind address.
        // Backend always listens on `port` (the internal port) at `bind` (127.0.0.1).
        // Caddy always listens on `httpsPort` (the public port) at 0.0.0.0.
        $backendCfgPort = (int)$object->get('port', 8080);
        $backendBindAddr = trim((string)$object->get('bind', '127.0.0.1'));
        $dataDir = rtrim($object->get('dataDir', '/opt/openmediavault/vmmanager'), '/');
        $configPath = $dataDir . '/config.json';
        if (is_readable($configPath) && is_writable($configPath)) {
            $config = json_decode(@file_get_contents($configPath), true);
            if (is_array($config)) {
                $config['values']['server.port'] = $backendCfgPort;
                $config['values']['server.bind_addr'] = $backendBindAddr;
                $config['saved_at'] = gmdate('Y-m-d\TH:i:s.v\Z');
                @file_put_contents($configPath, json_encode($config, JSON_PRETTY_PRINT));
            }
        }
        shell_exec('systemctl restart omv-vmmanager.service 2>&1');
        return $this->getSettings([], $context);
    }

    public function getStatus($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        $running = trim(shell_exec('systemctl is-active omv-vmmanager.service 2>/dev/null') ?? '');
        $enabled = trim(shell_exec('systemctl is-enabled omv-vmmanager.service 2>/dev/null') ?? '');
        $health  = null;
        if ($running === 'active') {
            try { $health = $this->backendGET('/api/health'); }
            catch (\Exception $e) { $health = ['error' => $e->getMessage()]; }
        }
        return [
            'service' => $running,
            'enabled' => ($enabled === 'enabled'),
            'version' => $health['version'] ?? null,
            'libvirt' => $health['libvirt'] ?? null,
        ];
    }

    public function getServiceStatus($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        $cfg = \OMV\Config\Database::getInstance()->get('conf.service.vmmanager');
        $running = trim(shell_exec('systemctl is-active omv-vmmanager.service 2>/dev/null') ?? '');
        $isActive = ($running === 'active');
        // Read actual backend port from config.json (the Go backend's source of truth).
        $dataDir = rtrim($cfg->get('dataDir', '/opt/openmediavault/vmmanager'), '/');
        $configPath = $dataDir . '/config.json';
        $backendPort = 8080;
        if (is_readable($configPath)) {
            $config = json_decode(@file_get_contents($configPath), true);
            if (is_array($config) && isset($config['values']['server.port'])) {
                $backendPort = (int)$config['values']['server.port'];
            }
        }
        // Build a label for the service state (Running v4d84df0 / Stopped / etc.)
        $system = null;
        if ($isActive) {
            try {
                $system = $this->backendGET('/api/system/status');
            } catch (\Exception $e) {
                $system = ['error' => $e->getMessage()];
            }
        }
        $stateLabel = $isActive ? 'Running' : 'Stopped';
        $versionLabel = '';
        $uptimeLabel = '';
        $libvirtLabel = '';
        $poolsLabel = '';
        if (is_array($system) && !isset($system['error'])) {
            $ver = $system['backend']['version'] ?? '';
            $stateLabel = $isActive ? ('Running v' . $ver) : 'Stopped';
            $uptimeSec = (int)($system['uptime_sec'] ?? 0);
            $uptimeLabel = $uptimeSec > 0 ? self::formatUptime($uptimeSec) : '';
            $lvConn = (bool)($system['libvirt']['connected'] ?? false);
            $lvUri = (string)($system['libvirt']['uri'] ?? '');
            $libvirtLabel = $lvConn ? ('Connected (' . $lvUri . ')') : 'Disconnected';
            $poolsCount = isset($system['pools']) && is_array($system['pools'])
                ? count($system['pools']) : 0;
            $poolsLabel = (string)$poolsCount;
        } elseif (is_array($system) && isset($system['error'])) {
            $libvirtLabel = 'Backend unreachable';
        }
        return [
            'enable'       => (bool)$cfg->get('enable', false),
            'running'      => $isActive,
            'running_label' => $stateLabel,
            'version'      => $versionLabel,
            'uptime'       => $uptimeLabel,
            'libvirt'      => $libvirtLabel,
            'pools_count'  => $poolsLabel,
            'backendPort'  => $backendPort,
            'httpsPort'    => (int)$cfg->get('httpsPort', 8443),
            'httpsMode'    => (string)$cfg->get('httpsMode', 'managed'),
        ];
    }

    private static function formatUptime($sec)
    {
        $d = intdiv($sec, 86400);
        $h = intdiv($sec % 86400, 3600);
        $m = intdiv($sec % 3600, 60);
        $parts = [];
        if ($d > 0) $parts[] = "{$d}d";
        if ($h > 0) $parts[] = "{$h}h";
        $parts[] = "{$m}m";
        return implode(' ', $parts);
    }

    public function getUIURL($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        $cfg  = \OMV\Config\Database::getInstance()->get('conf.service.vmmanager');
        $mode = $cfg->get('httpsMode', 'managed');
        $port = (int)$cfg->get('httpsPort', 8443);
        $host = gethostname();
        $scheme = ($mode !== 'none') ? 'https' : 'http';
        return ['url' => sprintf('%s://%s:%d/', $scheme, $host, $port)];
    }

    public function restartService($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        shell_exec('systemctl restart omv-vmmanager.service 2>&1');
        return ['status' => 'restarted'];
    }

    public function getLogs($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $lines = max(1, min((int)($params['lines'] ?? 100), 10000));
        $out = shell_exec(
            "journalctl -u omv-vmmanager.service -n {$lines} --no-pager 2>&1"
        ) ?? '';
        return ['output' => $out];
    }

    // -----------------------------------------------------------------
    // Proxies to Go backend
    // -----------------------------------------------------------------

    public function getHostStats($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        return $this->backendGET('/api/host/stats');
    }

    public function listVms($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        return $this->backendGET('/api/vms/');
    }

    public function getVmList($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.common.getlist');
        $vms = $this->backendGET('/api/vms/');
        if (!is_array($vms)) {
            $vms = [];
        }
        $objects = [];
        foreach ($vms as $vm) {
            $ramGb = sprintf('%.1f GB', ($vm['ram_mb'] ?? 0) / 1024);
            $objects[] = [
                'id'        => (string)($vm['id'] ?? ''),
                'name'      => (string)($vm['name'] ?? ''),
                'state'     => (string)($vm['state'] ?? 'unknown'),
                'vcpus'     => (int)($vm['vcpus'] ?? 0),
                'ram'       => $ramGb,
                'osversion' => (string)($vm['os_version'] ?? ''),
                'autostart' => !empty($vm['autostart']) ? _('Yes') : _('No'),
            ];
        }
        return $this->applyFilter($objects, $params['start'], $params['limit'],
            $params['sortfield'], $params['sortdir']);
    }

    public function vmCommand($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.vmcommand');
        $id      = rawurlencode($params['id']);
        $command = $params['command'];
        switch ($command) {
            case 'start':
                return $this->backendPOST('/api/vms/' . $id . '/start');
            case 'stop':
                return $this->backendPOST('/api/vms/' . $id . '/forceoff');
            case 'shutdown':
                return $this->backendPOST('/api/vms/' . $id . '/shutdown');
            case 'reboot':
                return $this->backendPOST('/api/vms/' . $id . '/reboot');
            case 'forcereboot':
                $this->backendPOST('/api/vms/' . $id . '/forceoff');
                return $this->backendPOST('/api/vms/' . $id . '/start');
            case 'suspend':
                return $this->backendPOST('/api/vms/' . $id . '/suspend');
            case 'resume':
                return $this->backendPOST('/api/vms/' . $id . '/resume');
            case 'delete':
                return $this->backendDELETE('/api/vms/' . $id);
            default:
                throw new \OMV\Rpc\Exception('Unknown VM command: ' . $command);
        }
    }

    public function listStoragePools($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        return $this->backendGET('/api/storage/pools');
    }

    public function listNetworks($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        return $this->backendGET('/api/networks/');
    }

    // -----------------------------------------------------------------
    // HTTP request helpers (POST, DELETE)
    // -----------------------------------------------------------------

    private function backendPOST($path, $body = null)
    {
        $ch = curl_init($this->backendURL($path));
        $headers = [
            'Authorization: Bearer ' . $this->getAPIKey(),
            'Accept: application/json',
        ];
        $opts = [
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER     => $headers,
            CURLOPT_TIMEOUT        => 8,
            CURLOPT_POST           => true,
        ];
        if ($body !== null) {
            $opts[CURLOPT_POSTFIELDS] = is_string($body) ? $body : json_encode($body);
            $headers[] = 'Content-Type: application/json';
            $opts[CURLOPT_HTTPHEADER] = $headers;
        }
        curl_setopt_array($ch, $opts);
        $resp = curl_exec($ch);
        $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $err  = curl_error($ch);
        curl_close($ch);
        if ($code === 0) {
            throw new \OMV\Rpc\Exception('Backend unreachable: ' . $err);
        }
        if ($code >= 400) {
            $detail = '';
            if (is_string($resp) && $resp !== '') {
                $decoded = json_decode($resp, true);
                if (is_array($decoded) && isset($decoded['error'])) {
                    $detail = ': ' . $decoded['error'];
                }
            }
            throw new \OMV\Rpc\Exception(sprintf(
                'Backend HTTP %d%s', $code, $detail
            ));
        }
        return json_decode($resp, true) ?? [];
    }

    private function backendDELETE($path)
    {
        $ch = curl_init($this->backendURL($path));
        curl_setopt_array($ch, [
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER     => [
                'Authorization: Bearer ' . $this->getAPIKey(),
                'Accept: application/json',
            ],
            CURLOPT_TIMEOUT   => 8,
            CURLOPT_CUSTOMREQUEST => 'DELETE',
        ]);
        $resp = curl_exec($ch);
        $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $err  = curl_error($ch);
        curl_close($ch);
        if ($code === 0) {
            throw new \OMV\Rpc\Exception('Backend unreachable: ' . $err);
        }
        if ($code >= 400) {
            $detail = '';
            if (is_string($resp) && $resp !== '') {
                $decoded = json_decode($resp, true);
                if (is_array($decoded) && isset($decoded['error'])) {
                    $detail = ': ' . $decoded['error'];
                }
            }
            throw new \OMV\Rpc\Exception(sprintf(
                'Backend HTTP %d%s', $code, $detail
            ));
        }
        return json_decode($resp, true) ?? [];
    }

    private function backendPUT($path, $body = null)
    {
        $ch = curl_init($this->backendURL($path));
        $headers = [
            'Authorization: Bearer ' . $this->getAPIKey(),
            'Accept: application/json',
        ];
        $opts = [
            CURLOPT_RETURNTRANSFER  => true,
            CURLOPT_HTTPHEADER      => $headers,
            CURLOPT_TIMEOUT         => 8,
            CURLOPT_CUSTOMREQUEST   => 'PUT',
        ];
        if ($body !== null) {
            $opts[CURLOPT_POSTFIELDS] = is_string($body) ? $body : json_encode($body);
            $headers[] = 'Content-Type: application/json';
            $opts[CURLOPT_HTTPHEADER] = $headers;
        }
        curl_setopt_array($ch, $opts);
        $resp = curl_exec($ch);
        $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $err  = curl_error($ch);
        curl_close($ch);
        if ($code === 0) {
            throw new \OMV\Rpc\Exception('Backend unreachable: ' . $err);
        }
        if ($code >= 400) {
            $detail = '';
            if (is_string($resp) && $resp !== '') {
                $decoded = json_decode($resp, true);
                if (is_array($decoded) && isset($decoded['error'])) {
                    $detail = ': ' . $decoded['error'];
                }
            }
            throw new \OMV\Rpc\Exception(sprintf(
                'Backend HTTP %d%s', $code, $detail
            ));
        }
        return json_decode($resp, true) ?? [];
    }

    // -----------------------------------------------------------------
    // Network management (proxy to Go backend + virsh for macvtap)
    // -----------------------------------------------------------------

    public function getNetworkList($params, $context)
    {
        // Validate the RPC caller context.
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        // Validate the parameters of the RPC service method.
        $this->validateMethodParams($params, 'rpc.common.getlist');
        // List networks with virsh (same pattern as KVM plugin).
        $cmd = new \OMV\System\Process('virsh', ['net-list', '--all', '--name']);
        $cmd->execute($output);
        $nets = array_values(array_filter(array_map('trim', $output)));
        $objects = [];
        foreach ($nets as $net) {
            $info = $this->virshGetNetworkInfo($net);
            $forward = '';
            if (!empty($info['forwarding']) && $info['forwarding'] !== 'None') {
                $forward = !empty($info['forward_dev'])
                    ? $info['forwarding'] . ' to ' . $info['forward_dev']
                    : $info['forwarding'];
            }
            $dhcp = 'Disabled';
            if (!empty($info['dhcp_start']) && !empty($info['dhcp_end'])) {
                $dhcp = $info['dhcp_start'] . ' - ' . $info['dhcp_end'];
            }
            $objects[] = [
                'netname' => $info['name'],
                'active'  => $info['active'] ? 'Active' : 'Inactive',
                'ip'      => $info['ip'],
                'iprange' => $info['ip_range'],
                'forward' => $forward,
                'dhcp'    => $dhcp,
            ];
        }
        // Filter the result.
        return $this->applyFilter($objects, $params['start'], $params['limit'],
            $params['sortfield'], $params['sortdir']);
    }

    private function virshGetNetworkInfo(string $net): array
    {
        $info = ['name' => $net, 'active' => false, 'ip' => '', 'ip_range' => '',
                 'forwarding' => '', 'forward_dev' => '', 'dhcp_start' => '', 'dhcp_end' => ''];
        // active status from net-info
        $escaped = escapeshellarg($net);
        $output = shell_exec("virsh net-info {$escaped} 2>/dev/null");
        if ($output && preg_match('/^Active:\s+(yes|no)/im', $output, $m)) {
            $info['active'] = (strtolower($m[1]) === 'yes');
        }
        // details from net-dumpxml
        $xml = shell_exec("virsh net-dumpxml {$escaped} 2>/dev/null");
        if (!$xml) return $info;
        try {
            $xmlObj = new SimpleXMLElement($xml);
            if (isset($xmlObj->forward)) {
                $info['forwarding']  = (string)($xmlObj->forward['mode'] ?? 'None');
                $info['forward_dev'] = (string)($xmlObj->forward->interface['dev'] ?? '');
            }
            if (isset($xmlObj->ip)) {
                $addr = (string)($xmlObj->ip['address'] ?? '');
                $mask = (string)($xmlObj->ip['netmask'] ?? '');
                $info['ip']       = $addr;
                $info['ip_range'] = $mask ? sprintf('%s/%s', $addr, $mask) : $addr;
                if (isset($xmlObj->ip->dhcp->range)) {
                    $info['dhcp_start'] = (string)($xmlObj->ip->dhcp->range['start'] ?? '');
                    $info['dhcp_end']   = (string)($xmlObj->ip->dhcp->range['end'] ?? '');
                }
            }
        } catch (\Exception $e) {}
        return $info;
    }

    public function getNetwork($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.getnetwork');
        $raw = $this->virshGetNetworkInfo($params['name']);
        $forward = $raw['forwarding'];
        if ($forward === 'None' || $forward === '') {
            // Check if it has a bridge but no forward = isolated/open
            $forward = 'isolated';
        }
        $forward = strtolower($forward);
        $subnet = '';
        $cidrPrefix = 24;
        if (!empty($raw['ip_range'])) {
            $parts = explode('/', $raw['ip_range'], 2);
            if (count($parts) === 2) {
                $mask = trim($parts[1]);
                $cidrPrefix = ctype_digit($mask) ? (int)$mask : $this->netmaskToPrefix($mask);
                $subnet = long2ip(-1 << (32 - $cidrPrefix));
            }
        }
        return [
            'name'         => $params['name'],
            'forward'      => $forward,
            'gatewayip'    => $raw['ip'],
            'subnet'       => $subnet,
            'dhcp'         => (!empty($raw['dhcp_start']) && !empty($raw['dhcp_end'])),
            'startaddress' => $raw['dhcp_start'],
            'endaddress'   => $raw['dhcp_end'],
            'macaddress'   => '',
        ];
    }

    public function setNetwork($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.setnetwork');
        $forward = $params['forward'];
        $needsSubnet = in_array($forward, ['nat', 'route'], true);
        // Build the body for the Go backend.
        $body = [
            'name'    => $params['name'],
            'forward' => $forward,
        ];
        if ($needsSubnet) {
            if (empty($params['gatewayip']) || empty($params['subnet'])) {
                throw new \OMV\Rpc\Exception(
                    'Forwarding "' . $forward . '" requires a gateway IP and a subnet mask.'
                );
            }
            $cidr = sprintf('%s/%d',
                $params['gatewayip'],
                $this->netmaskToPrefix($params['subnet'])
            );
            $this->assertNoSubnetConflict($params['name'], $cidr);
            $body['cidr'] = $cidr;
        }
        if (!empty($params['dhcp'])) {
            if (empty($params['startaddress']) || empty($params['endaddress'])) {
                throw new \OMV\Rpc\Exception(
                    'DHCP requires both a starting and an ending address.'
                );
            }
            $startLong = ip2long($params['startaddress']);
            $endLong   = ip2long($params['endaddress']);
            if ($startLong === false || $endLong === false) {
                throw new \OMV\Rpc\Exception('Invalid DHCP range IP address.');
            }
            if ($startLong >= $endLong) {
                throw new \OMV\Rpc\Exception(
                    'DHCP starting address must be less than the ending address.'
                );
            }
            if ($needsSubnet) {
                $this->assertInSubnet(
                    $params['startaddress'], $body['cidr'], 'DHCP starting address'
                );
                $this->assertInSubnet(
                    $params['endaddress'], $body['cidr'], 'DHCP ending address'
                );
            }
            $body['dhcp'] = true;
            $body['dhcp_start'] = $params['startaddress'];
            $body['dhcp_end']   = $params['endaddress'];
        } else {
            $body['dhcp'] = false;
        }
        $body['autostart'] = true;
        return $this->backendPOST('/api/networks/', $body);
    }

    /**
     * Throw if there is already a libvirt network with $name, or if $cidr
     * overlaps with any existing libvirt network's CIDR. "default" and the
     * auto-created "vmmanager-bridge" are also considered.
     */
    private function assertNoSubnetConflict($name, $cidr, $skipNameCheck = false)
    {
        $cmd = new \OMV\System\Process('virsh', ['net-list', '--all', '--name']);
        $cmd->execute($output);
        foreach (array_map('trim', $output) as $net) {
            if ($net === '') continue;
            if ($net === $name) {
                if ($skipNameCheck) continue; // skip this network entirely
                throw new \OMV\Rpc\Exception(
                    'A network named "' . $name . '" already exists.'
                );
            }
            $info = $this->virshGetNetworkInfo($net);
            if (empty($info['ip']) || empty($info['ip_range'])) {
                continue;
            }
            $existingCidr = $this->ipRangeToCidr(
                $info['ip'], $info['ip_range']
            );
            if ($existingCidr === null) continue;
            if ($this->cidrsOverlap($cidr, $existingCidr)) {
                throw new \OMV\Rpc\Exception(sprintf(
                    'Subnet %s overlaps with existing network "%s" (%s). ' .
                    'Choose a different subnet.',
                    $cidr, $net, $existingCidr
                ));
            }
        }
    }

    /**
     * Convert the virshGetNetworkInfo output (ip + ip_range like
     * "192.168.122.1/255.255.255.0" or "192.168.122.1/24") to a CIDR
     * string. Returns null if either part is missing or malformed.
     */
    private function ipRangeToCidr($ip, $ipRange)
    {
        if (empty($ip) || empty($ipRange)) return null;
        $parts = explode('/', $ipRange, 2);
        if (count($parts) !== 2) return null;
        $mask = trim($parts[1]);
        $prefix = ctype_digit($mask) ? (int)$mask : $this->netmaskToPrefix($mask);
        if ($prefix < 0 || $prefix > 32) return null;
        return $ip . '/' . $prefix;
    }

    /**
     * Return true if two CIDR networks overlap. They overlap iff the
     * narrower prefix is fully contained in the wider one. The wider
     * network's base AND mask equals the narrower IP AND the wider
     * network's base AND mask.
     */
    private function cidrsOverlap($cidr1, $cidr2)
    {
        list($ip1, $p1) = explode('/', $cidr1, 2);
        list($ip2, $p2) = explode('/', $cidr2, 2);
        $ip1l = ip2long($ip1);
        $ip2l = ip2long($ip2);
        if ($ip1l === false || $ip2l === false) return false;
        $p1 = (int)$p1;
        $p2 = (int)$p2;
        if ($p1 <= $p2) {
            // cidr1 is wider (or equal). Check whether ip2 falls in it.
            $mask = $p1 === 0 ? 0 : ((-1 << (32 - $p1)) & 0xFFFFFFFF);
            return ($ip2l & $mask) === ($ip1l & $mask);
        } else {
            $mask = $p2 === 0 ? 0 : ((-1 << (32 - $p2)) & 0xFFFFFFFF);
            return ($ip1l & $mask) === ($ip2l & $mask);
        }
    }

    private function assertInSubnet($ip, $cidr, $fieldName)
    {
        [$net, $prefix] = explode('/', $cidr, 2);
        $ipLong = ip2long($ip);
        $netLong = ip2long($net);
        if ($ipLong === false || $netLong === false) {
            throw new \OMV\Rpc\Exception(
                $fieldName . ': invalid IP address.'
            );
        }
        $mask = (int)$prefix === 0 ? 0 : ((-1 << (32 - (int)$prefix)) & 0xFFFFFFFF);
        if (($ipLong & $mask) !== ($netLong & $mask)) {
            throw new \OMV\Rpc\Exception(sprintf(
                '%s (%s) is not within the chosen subnet %s.',
                $fieldName, $ip, $cidr
            ));
        }
    }

    public function updateNetwork($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.updatenetwork');
        $name = $params['name'];
        $forward = $params['forward'];
        $needsSubnet = in_array($forward, ['nat', 'route'], true);
        $body = [
            'name'    => $name,
            'forward' => $forward,
        ];
        if ($needsSubnet) {
            if (empty($params['gatewayip']) || empty($params['subnet'])) {
                throw new \OMV\Rpc\Exception(
                    'Forwarding "' . $forward . '" requires a gateway IP and a subnet mask.'
                );
            }
            $cidr = sprintf('%s/%d',
                $params['gatewayip'],
                $this->netmaskToPrefix($params['subnet'])
            );
            $this->assertNoSubnetConflict($params['name'], $cidr, true);
            $body['cidr'] = $cidr;
        }
        if (!empty($params['dhcp'])) {
            if (empty($params['startaddress']) || empty($params['endaddress'])) {
                throw new \OMV\Rpc\Exception(
                    'DHCP requires both a starting and an ending address.'
                );
            }
            $startLong = ip2long($params['startaddress']);
            $endLong   = ip2long($params['endaddress']);
            if ($startLong === false || $endLong === false) {
                throw new \OMV\Rpc\Exception('Invalid DHCP range IP address.');
            }
            if ($startLong >= $endLong) {
                throw new \OMV\Rpc\Exception(
                    'DHCP starting address must be less than the ending address.'
                );
            }
            if ($needsSubnet) {
                $this->assertInSubnet(
                    $params['startaddress'], $body['cidr'], 'DHCP starting address'
                );
                $this->assertInSubnet(
                    $params['endaddress'], $body['cidr'], 'DHCP ending address'
                );
            }
            $body['dhcp'] = true;
            $body['dhcp_start'] = $params['startaddress'];
            $body['dhcp_end']   = $params['endaddress'];
        } else {
            $body['dhcp'] = false;
        }
        if (!empty($params['macaddress'])) {
            $body['mac'] = $params['macaddress'];
        }
        return $this->backendPUT('/api/networks/' . rawurlencode($name), $body);
    }

    public function setBridgeNetwork($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.setbridgenetwork');
        $body = [
            'name'    => $params['name'],
            'forward' => 'bridge',
            'bridge'  => $params['bridge'],
        ];
        return $this->backendPOST('/api/networks/', $body);
    }

    public function setMacvtap($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.setmacvtap');
        // macvtap is not supported by the Go backend — define via virsh directly
        $name = htmlspecialchars($params['name'], ENT_XML1 | ENT_QUOTES, 'UTF-8');
        $nic  = htmlspecialchars($params['nic'], ENT_XML1 | ENT_QUOTES, 'UTF-8');
        $mode = $params['mode'] ?? 'bridge';
        $xml = sprintf(
            '<network><name>%s</name><forward mode="%s"><interface dev="%s"/></forward></network>',
            $name, $mode, $nic
        );
        $tmp = tempnam(sys_get_temp_dir(), 'vmmanager_macvtap_');
        file_put_contents($tmp, $xml);
        $cmd = sprintf('virsh net-define %s 2>&1', escapeshellarg($tmp));
        $out = shell_exec($cmd);
        unlink($tmp);
        if ($out !== null && $out !== '') {
            throw new \OMV\Rpc\Exception('virsh net-define: ' . $out);
        }
        // Start the network and set autostart
        shell_exec(sprintf('virsh net-start %s 2>&1', escapeshellarg($name)));
        shell_exec(sprintf('virsh net-autostart %s 2>&1', escapeshellarg($name)));
    }

    public function networkCommand($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.networkcommand');
        $name = $params['name'];
        switch ($params['command']) {
            case 'delete':
                $this->backendDELETE('/api/networks/' . rawurlencode($name));
                return;
            case 'start':
                $this->backendPOST('/api/networks/' . rawurlencode($name) . '/start');
                break;
            case 'stop':
                $this->backendPOST('/api/networks/' . rawurlencode($name) . '/stop');
                break;
            default:
                throw new \OMV\Rpc\Exception('Unknown network command: ' . $params['command']);
        }
    }

    public function enumerateBridges($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $objects = [];
        foreach (glob('/sys/class/net/br*', GLOB_ONLYDIR) as $path) {
            $bridge = basename($path);
            // Skip Docker-managed bridges (br-xxxxxxxxxxxx)
            if (preg_match('/^br-[0-9a-f]{12}$/', $bridge)) {
                continue;
            }
            $objects[] = ['bridge' => $bridge];
        }
        return $objects;
    }

    public function getNetworkXml($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.networkname');
        $name = escapeshellarg($params['name']);
        $xml = shell_exec("virsh net-dumpxml {$name} 2>/dev/null");
        if ($xml === null || $xml === '') {
            throw new \OMV\Rpc\Exception('Failed to get network XML for: ' . $params['name']);
        }
        return ['netxml' => $xml];
    }

    public function putNetworkXml($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.putnetworkxml');
        $name = $params['name'];
        $xml = $params['netxml'];
        // Basic XML validation
        if (empty($xml)) {
            throw new \OMV\Rpc\Exception(_('XML cannot be empty.'));
        }
        if (strlen($xml) < 20 || strlen($xml) > 1048576) {
            throw new \OMV\Rpc\Exception(_('Invalid XML size.'));
        }
        if (!preg_match('#(?:<network\s|<network>)#i', $xml)) {
            throw new \OMV\Rpc\Exception(_('XML must start with <network>.'));
        }
        // Ensure the name in XML matches
        if (preg_match('#<name>\s*(.+?)\s*</name>#', $xml, $m)) {
            $xmlName = trim($m[1]);
            if ($xmlName !== $name) {
                throw new \OMV\Rpc\Exception(sprintf(
                    'XML name "%s" must match network name "%s".', 
                    $xmlName, $name
                ));
            }
        }
        // Write XML to tmp file
        $tmp = tempnam(sys_get_temp_dir(), 'vmmanager_network_');
        file_put_contents($tmp, $xml);
        $escaped = escapeshellarg($name);
        try {
            // Define the network via virsh
            $out = shell_exec(sprintf('virsh net-define %s 2>&1', escapeshellarg($tmp)));
            if (!empty($out)) {
                throw new \OMV\Rpc\Exception('virsh: ' . $out);
            }
            // Ensure autostart is set
            shell_exec(sprintf('virsh net-autostart %s 2>&1', $escaped));
            // Restart if network is active
            $info = shell_exec(sprintf('virsh net-info %s 2>/dev/null', $escaped));
            if ($info && strpos($info, 'Active:     yes') !== false) {
                shell_exec(sprintf('virsh net-destroy %s', $escaped));
                shell_exec(sprintf('virsh net-start %s', $escaped));
            }
        } finally {
            unlink($tmp);
        }
    }

    // -----------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------

    private function netmaskToPrefix($mask)
    {
        $long = ip2long($mask);
        if ($long === false) return 24;
        $base = ip2long('255.255.255.255');
        $bits = 32 - substr_count(decbin($long ^ $base), '1');
        return $bits;
    }

    // -----------------------------------------------------------------
    // Pool management (proxy to Go backend)
    // -----------------------------------------------------------------

    public function getPoolList($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_EVERYONE]);
        $this->validateMethodParams($params, 'rpc.common.getlist');
        $pools = $this->backendGET('/api/storage/pools');
        if (!is_array($pools)) {
            $pools = [];
        }
        $objects = [];
        foreach ($pools as $p) {
            $objects[] = [
                'name'      => (string)($p['name'] ?? ''),
                'purpose'   => (string)($p['purpose'] ?? 'disk'),
                'type'      => (string)($p['type'] ?? 'dir'),
                'path'      => (string)($p['path'] ?? ''),
                'state'     => (string)($p['state'] ?? 'inactive'),
                'autostart' => (bool)($p['autostart'] ?? false),
                'capacity'  => (int)($p['capacity'] ?? 0),
                'allocated' => (int)($p['allocated'] ?? 0),
                'available' => (int)($p['available'] ?? 0),
            ];
        }
        return $this->applyFilter($objects, $params['start'], $params['limit'],
            $params['sortfield'], $params['sortdir']);
    }

    public function createPool($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.createpool');
        $body = [
            'name'    => $params['name'],
            'type'    => $params['type'],
            'path'    => $params['path'],
            'purpose' => $params['purpose'] ?? 'disk',
        ];
        if (!empty($params['source_host'])) {
            $body['source_host']   = $params['source_host'];
            $body['source_dir']    = $params['source_dir'] ?? '';
            $body['source_format'] = $params['source_format'] ?? 'nfs';
        }
        if (!empty($params['source_username']) && !empty($params['source_password'])) {
            $body['source_username'] = $params['source_username'];
            $body['source_password'] = $params['source_password'];
        }
        return $this->backendPOST('/api/storage/pools', $body);
    }

    public function updatePool($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.updatepool');
        $body = [];
        if (array_key_exists('path', $params) && $params['path'] !== '') {
            $body['path'] = $params['path'];
        }
        if (!empty($params['source_username']) && !empty($params['source_password'])) {
            $body['source_username'] = $params['source_username'];
            $body['source_password'] = $params['source_password'];
        }
        if (!empty($params['cifs_needs_reauth'])) {
            $body['cifs-needs-reauth'] = true;
        }
        if (empty($body)) {
            throw new \OMV\Rpc\Exception(
                'No update fields provided (path, source credentials, or cifs-needs-reauth).'
            );
        }
        return $this->backendPUT('/api/storage/pools/' . rawurlencode($params['name']), $body);
    }

    public function deletePool($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.poolname');
        return $this->backendDELETE('/api/storage/pools/' . rawurlencode($params['name']));
    }

    public function poolCommand($params, $context)
    {
        $this->validateMethodContext($context, ['role' => OMV_ROLE_ADMINISTRATOR]);
        $this->validateMethodParams($params, 'rpc.vmmanager.poolcommand');
        $name    = $params['name'];
        $command = $params['command'];
        switch ($command) {
            case 'start':
                return $this->backendPOST('/api/storage/pools/' . rawurlencode($name) . '/start');
            case 'stop':
                return $this->backendPOST('/api/storage/pools/' . rawurlencode($name) . '/stop');
            case 'refresh':
                return $this->backendPOST('/api/storage/pools/' . rawurlencode($name) . '/refresh');
            case 'delete':
                return $this->backendDELETE('/api/storage/pools/' . rawurlencode($name));
            case 'fixpermissions':
                // Local action: chown/chmod the pool path so libvirt-qemu
                // can read it. The backend's fixpermissions is wired to
                // /api/storage/pools/{name}/fixpermissions; if not present
                // we do the simple local fallback.
                $cfg = \OMV\Config\Database::getInstance()->get('conf.service.vmmanager');
                $dataDir = $cfg->get('dataDir', '/opt/openmediavault/vmmanager');
                $info = $this->backendGET('/api/storage/pools/' . rawurlencode($name));
                $path = $info['path'] ?? ($dataDir . '/pools/' . $name);
                if (!is_dir($path)) {
                    throw new \OMV\Rpc\Exception(
                        'Pool path does not exist: ' . $path
                    );
                }
                shell_exec(sprintf(
                    'chmod 0755 %s && chown root:libvirt-qemu %s 2>&1',
                    escapeshellarg($path), escapeshellarg($path)
                ));
                return ['status' => 'ok'];
            default:
                throw new \OMV\Rpc\Exception('Unknown pool command: ' . $command);
        }
    }
}
