package api

import (
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// serverIP returns the IP address that should be baked into
// console-related files the user downloads (.rdp, .vv) and that
// the noVNC WebSocket should use as a default. In a normal
// systemd install, the first non-loopback IPv4 of the running
// process is the right answer. In Docker, that address is the
// container's bridge IP (e.g. 172.30.0.2), which the user's
// browser cannot reach, so the Docker compose file overrides the
// answer via the PUBLIC_HOST env var.
func (h *Handler) serverIP() string {
	if h.cfg != nil && h.cfg.PublicHost != "" {
		return h.cfg.PublicHost
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		host, _ := os.Hostname()
		return host
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	host, _ := os.Hostname()
	return host
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Handler) GetGraphics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	info, err := h.lv.GetVNCInfo(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	jsonResp(w, http.StatusOK, info)
}

func (h *Handler) VNCProxy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	info, err := h.lv.GetVNCInfo(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}

	// The VNC port is bound on the host that runs libvirtd. In a
	// normal systemd install that's the same host as this backend,
	// so 127.0.0.1 works. In Docker, libvirtd runs on the host and
	// this process is in a separate network namespace, so we have
	// to dial the host's address. The default is 127.0.0.1; the
	// Docker compose file overrides VNC_PROXY_HOST with the bridge
	// gateway (e.g. 172.30.0.1 for the default br-vmmanager net).
	vncHost := h.cfg.VNCProxyHost
	if vncHost == "" {
		vncHost = "127.0.0.1"
	}
	vncAddr := fmt.Sprintf("%s:%d", vncHost, info.Port)
	slog.Info("vnc_proxy_dialing", "vm_id", id, "libvirt_host", info.Host, "libvirt_port", info.Port, "vnc_addr", vncAddr)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("vnc_proxy_upgrade_failed", "err", err)
		jsonErr(w, http.StatusInternalServerError, "websocket upgrade failed")
		return
	}
	defer ws.Close()

	tcpConn, err := net.DialTimeout("tcp", vncAddr, 10*time.Second)
	if err != nil {
		slog.Error("vnc_proxy_dial_failed", "addr", vncAddr, "err", err)
		jsonErr(w, http.StatusBadGateway, fmt.Sprintf("vnc connection failed: %v", err))
		return
	}
	defer tcpConn.Close()
	slog.Info("vnc_proxy_connected", "addr", vncAddr)

	ws.SetCloseHandler(func(code int, text string) error {
		tcpConn.Close()
		return nil
	})

	ws.SetReadLimit(1 << 20)

	errc := make(chan error, 2)

	go func() {
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if _, err := tcpConn.Write(msg); err != nil {
				errc <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := tcpConn.Read(buf)
			if err != nil {
				errc <- err
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				errc <- err
				return
			}
		}
	}()

	<-errc
}

func (h *Handler) ConsolePage(w http.ResponseWriter, r *http.Request) {
	id := html.EscapeString(chi.URLParam(r, "id"))

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Console - %s</title>
<style>
:root{--indigo-400:#818cf8;--indigo-500:#6366f1;--indigo-600:#4f46e5;--indigo-700:#4338ca;--slate-800:#1e293b;--slate-850:#0f1729;--slate-900:#020617;--slate-950:#010101}
*{margin:0;padding:0;box-sizing:border-box}
html,body{background:var(--slate-900);overflow:hidden;height:100%%;width:100%%;font-family:system-ui,-apple-system,sans-serif}
#screen{width:100%%;height:100%%}
canvas{display:block;margin:auto}
/* ---- sidebar ---- */
#sidebar{position:fixed;left:0;top:50%%;transform:translateY(-50%%);display:flex;flex-direction:column;gap:2px;padding:6px;border-radius:0 12px 12px 0;background:rgba(2,6,23,.75);backdrop-filter:blur(16px);border:1px solid rgba(99,102,241,.1);border-left:none;box-shadow:4px 0 24px rgba(0,0,0,.3);z-index:20;opacity:0;transition:opacity .3s}
#sidebar.visible{opacity:1}
#sidebar button{display:flex;align-items:center;justify-content:center;width:36px;height:36px;border:none;border-radius:8px;background:transparent;color:#64748b;cursor:pointer;transition:all .15s;position:relative}
#sidebar button:hover{background:rgba(99,102,241,.12);color:#c7d2fe}
#sidebar button.active{background:rgba(99,102,241,.18);color:var(--indigo-400)}
#sidebar button svg{width:18px;height:18px}
#sidebar button .badge{position:absolute;top:2px;right:2px;width:7px;height:7px;border-radius:50%%;background:#ef4444}
#sidebar .sep{height:1px;margin:4px 10px;background:rgba(148,163,184,.08)}
#sidebar .reconnect{color:#ef4444}
#sidebar .reconnect:hover{background:rgba(239,68,68,.15);color:#fca5a5}
/* ---- panel ---- */
#panel{position:fixed;left:48px;top:50%%;transform:translateY(-50%%) translateX(-320px);width:280px;max-height:420px;border-radius:12px;background:rgba(2,6,23,.82);backdrop-filter:blur(16px);border:1px solid rgba(99,102,241,.12);box-shadow:0 8px 40px rgba(0,0,0,.5);z-index:19;transition:transform .25s cubic-bezier(.22,1,.36,1);overflow:hidden;display:flex;flex-direction:column}
#panel.open{transform:translateY(-50%%) translateX(0)}
#panel .phead{display:flex;align-items:center;justify-content:space-between;padding:12px 14px 8px;border-bottom:1px solid rgba(148,163,184,.08)}
#panel .phead h3{font-size:13px;font-weight:600;color:#e2e8f0;letter-spacing:.02em}
#panel .phead .close{background:none;border:none;color:#64748b;cursor:pointer;padding:2px;border-radius:4px}
#panel .phead .close:hover{color:#e2e8f0;background:rgba(148,163,184,.1)}
#panel .pbody{padding:12px 14px 14px;overflow-y:auto;flex:1}
#panel .pbody .kbtn{display:flex;align-items:center;gap:10px;width:100%%;padding:9px 12px;margin-bottom:4px;border:1px solid rgba(148,163,184,.08);border-radius:8px;background:rgba(255,255,255,.03);color:#94a3b8;cursor:pointer;font:12px/1 system-ui,sans-serif;transition:all .12s}
#panel .pbody .kbtn:hover{background:rgba(99,102,241,.12);border-color:rgba(99,102,241,.2);color:#e2e8f0}
#panel .pbody .kbtn:active{transform:scale(.97)}
#panel .pbody .kbtn .kicon{width:28px;height:28px;display:flex;align-items:center;justify-content:center;border-radius:6px;background:rgba(99,102,241,.1);flex-shrink:0}
#panel .pbody .kbtn .kicon svg{width:14px;height:14px;color:var(--indigo-400)}
#panel .pbody .kbtn .klabel{flex:1}
#panel .pbody .kbtn .kkeys{font-size:10px;color:#64748b;background:rgba(148,163,184,.06);padding:2px 7px;border-radius:4px;font-family:monospace}
#panel .pbody .clabel{font-size:11px;color:#64748b;margin-bottom:6px}
#panel .pbody textarea{width:100%%;height:80px;padding:10px 12px;border:1px solid rgba(148,163,184,.12);border-radius:8px;background:rgba(0,0,0,.3);color:#e2e8f0;font:12px/1.4 monospace;resize:none;outline:none;transition:border .15s}
#panel .pbody textarea:focus{border-color:rgba(99,102,241,.4)}
#panel .pbody textarea::placeholder{color:#475569}
#panel .pbody .cbtns{display:flex;gap:6px;margin-top:6px}
#panel .pbody .cbtns button{flex:1;padding:6px;border:1px solid rgba(148,163,184,.1);border-radius:6px;background:rgba(255,255,255,.03);color:#94a3b8;cursor:pointer;font:11px/1 system-ui,sans-serif;transition:all .12s}
#panel .pbody .cbtns button:hover{background:rgba(99,102,241,.12);color:#e2e8f0}
#panel .pbody .cbtns button.primary{background:rgba(99,102,241,.15);border-color:rgba(99,102,241,.25);color:var(--indigo-400)}
#panel .pbody .cbtns button.primary:hover{background:rgba(99,102,241,.25)}
#panel .pbody .srow{display:flex;align-items:center;justify-content:space-between;padding:8px 0}
#panel .pbody .srow+.srow{border-top:1px solid rgba(148,163,184,.05)}
#panel .pbody .srow .slabel{font-size:12px;color:#94a3b8}
#panel .pbody .srow .sdesc{font-size:10px;color:#64748b;margin-top:1px}
#panel .pbody .srow .stoggle{position:relative;width:36px;height:20px;flex-shrink:0;border-radius:10px;background:rgba(148,163,184,.15);cursor:pointer;transition:background .2s}
#panel .pbody .srow .stoggle.on{background:rgba(99,102,241,.5)}
#panel .pbody .srow .stoggle .knob{position:absolute;top:2px;left:2px;width:16px;height:16px;border-radius:50%%;background:#94a3b8;transition:all .2s}
#panel .pbody .srow .stoggle.on .knob{left:18px;background:#e2e8f0;box-shadow:0 0 8px rgba(99,102,241,.3)}
#panel .pbody .irow{display:flex;justify-content:space-between;padding:5px 0;font-size:12px}
#panel .pbody .irow .ilabel{color:#64748b}
#panel .pbody .irow .ivalue{color:#94a3b8;font-family:monospace;font-size:11px}
/* ---- status ---- */
#status{position:fixed;bottom:20px;right:20px;padding:6px 14px 6px 10px;border-radius:20px;font:11px/1.4 monospace;color:#94a3b8;background:rgba(2,6,23,.7);backdrop-filter:blur(8px);border:1px solid rgba(99,102,241,.08);pointer-events:none;transition:all .3s;display:flex;align-items:center;gap:6px}
#status .dot{width:6px;height:6px;border-radius:50%%;flex-shrink:0}
#status .dot.connecting{background:#facc15;animation:pulse 1.2s infinite}
#status .dot.ok{background:#22c55e;box-shadow:0 0 6px rgba(34,197,94,.4)}
#status .dot.error{background:#ef4444;box-shadow:0 0 6px rgba(239,68,68,.4)}
@keyframes pulse{0%%,100%%{opacity:1}50%%{opacity:.3}}
@keyframes fadeIn{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:translateY(0)}}
</style>
</head>
<body>
<div id="sidebar" class="visible">
  <button id="btnFullscreen" title="Fullscreen (F11)">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/></svg>
  </button>
  <div class="sep"></div>
  <button id="btnToggleKeys" title="Keyboard shortcuts">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M6 8h.01"/><path d="M10 8h.01"/><path d="M14 8h.01"/><path d="M18 8h.01"/><path d="M6 12h.01"/><path d="M18 12h.01"/><path d="M6 16h12"/></svg>
  </button>
  <button id="btnToggleClip" title="Clipboard">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1" ry="1"/></svg>
  </button>
  <button id="btnToggleSettings" title="Settings">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
  </button>
  <button id="btnToggleInfo" title="Connection info">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
  </button>
  <div class="sep"></div>
  <button id="btnReconnect" class="reconnect" title="Reconnect" style="display:none">
    <svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M21 12a9 9 0 0 1-9 9m9-9a9 9 0 0 0-9-9m9 9H3m9 9a9 9 0 0 1-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 0 1 9-9"/></svg>
  </button>
</div>
<div id="panel">
  <div class="phead">
    <h3 id="panelTitle"></h3>
    <button class="close" id="panelClose"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24" width="16" height="16"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg></button>
  </div>
  <div class="pbody" id="panelBody"></div>
</div>
<div id="screen"></div>
<div id="status"><span class="dot connecting"></span>Connecting</div>
<script type="module">
import RFB from '/static/novnc.mjs';

history.pushState(null, null, location.href);
window.onpopstate = function () {
    history.pushState(null, null, location.href);
};

var host = window.location.hostname;
var port = window.location.port || (window.location.protocol === 'https:' ? '443' : '80');
var wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
var url = wsProto + '//' + host + ':' + port + '/api/vms/%s/vnc';

var rfb = null, connected = false, activePanel = null;
var statusEl = document.getElementById('status');
var sidebar = document.getElementById('sidebar');
var panel = document.getElementById('panel');
var panelTitle = document.getElementById('panelTitle');
var panelBody = document.getElementById('panelBody');

function updateStatus(state, msg) {
    var dot = statusEl.querySelector('.dot');
    dot.className = 'dot ' + state;
    statusEl.childNodes[1].nodeValue = ' ' + msg;
}

function sendKeyCombo(keys) {
    if (!rfb || !connected) return;
    for (var i = 0; i < keys.length; i++)
        rfb.sendKey(keys[i][0], keys[i][1], true);
    setTimeout(function () {
        for (var i = keys.length - 1; i >= 0; i--)
            rfb.sendKey(keys[i][0], keys[i][1], false);
    }, 80);
}

function openPanel(name, title, html) {
    panelTitle.textContent = title;
    panelBody.innerHTML = html;
    panel.classList.add('open');
    activePanel = name;
    document.querySelectorAll('#sidebar button').forEach(function (b) {
        b.classList.toggle('active', b.id === 'btnToggle' + name.charAt(0).toUpperCase() + name.slice(1));
    });
}

function closePanel() {
    panel.classList.remove('open');
    activePanel = null;
    document.querySelectorAll('#sidebar button').forEach(function (b) { b.classList.remove('active'); });
}

/* ---- keys panel ---- */
document.getElementById('btnToggleKeys').onclick = function () {
    if (activePanel === 'keys') { closePanel(); return; }
    var keysHtml =
      '<div class="kbtn" data-combo=\'[[65507,"ControlLeft"],[65513,"AltLeft"],[65535,"Delete"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M6 8h.01"/><path d="M10 8h.01"/><path d="M14 8h.01"/><path d="M18 8h.01"/><path d="M8 12h8"/><path d="M10 16h4"/></svg></div><span class="klabel">Ctrl+Alt+Del</span><span class="kkeys">Ctrl+Alt+Del</span></div>' +
      '<div class="kbtn" data-combo=\'[[65507,"ControlLeft"],[65307,"Escape"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M6 8h.01"/><path d="M10 8h.01"/><path d="M14 8h.01"/><path d="M18 8h.01"/><path d="M12 12v4"/></svg></div><span class="klabel">Ctrl+Esc</span><span class="kkeys">Ctrl+Esc</span></div>' +
      '<div class="kbtn" data-combo=\'[[65513,"AltLeft"],[65289,"Tab"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="m8 12 4-4 4 4"/><path d="M12 8v8"/></svg></div><span class="klabel">Alt+Tab</span><span class="kkeys">Alt+Tab</span></div>' +
      '<div class="kbtn" data-combo=\'[[65513,"AltLeft"],[65289,"Tab"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><path d="M17 20H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2z"/><path d="M9 8h6"/></svg></div><span class="klabel">Alt+Shift+Tab</span><span class="kkeys">Alt+Shift+Tab</span></div>' +
      '<div class="kbtn" data-combo=\'[[65515,"MetaLeft"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="2" width="8" height="8" rx="1"/><rect x="14" y="2" width="8" height="8" rx="1"/><rect x="2" y="14" width="8" height="8" rx="1"/><rect x="14" y="14" width="8" height="8" rx="1"/></svg></div><span class="klabel">Super (Windows key)</span><span class="kkeys">Super</span></div>' +
      '<div class="kbtn" data-combo=\'[[65513,"AltLeft"],[65473,"F4"]]\'><div class="kicon"><svg fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" viewBox="0 0 24 24"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="m9 12 6-6"/><path d="m15 12-6-6"/></svg></div><span class="klabel">Alt+F4</span><span class="kkeys">Alt+F4</span></div>';
    openPanel('keys', 'Keyboard', keysHtml);
    panelBody.querySelectorAll('.kbtn').forEach(function (btn) {
        btn.onclick = function () {
            var combo = JSON.parse(btn.getAttribute('data-combo'));
            sendKeyCombo(combo);
        };
    });
};

/* ---- seamless clipboard ---- */
var lastClipText = '';

function syncClipToRemote() {
    if (!rfb || !connected) return;
    try {
        navigator.clipboard.readText().then(function (text) {
            if (text && text !== lastClipText) {
                lastClipText = text;
                rfb.clipboardPasteFrom(text);
                var ta = document.getElementById('clipText');
                if (ta) ta.value = text;
            }
        }).catch(function () {});
    } catch(e) {}
}

function syncClipToLocal(text) {
    if (!text || text === lastClipText) return;
    lastClipText = text;
    var ta = document.getElementById('clipText');
    if (ta) ta.value = text;
    try {
        navigator.clipboard.writeText(text).catch(function () {});
    } catch(e) {}
}

document.addEventListener('copy', function () {
    setTimeout(syncClipToRemote, 100);
});
document.addEventListener('cut', function () {
    setTimeout(syncClipToRemote, 100);
});

/* ---- clipboard panel (fallback) ---- */
document.getElementById('btnToggleClip').onclick = function () {
    if (activePanel === 'clip') { closePanel(); return; }
    var clipHtml =
      '<div class="clabel">Clipboard (sincronizado automáticamente)</div>' +
      '<textarea id="clipText" placeholder="Clipboard text..."></textarea>';
    openPanel('clip', 'Clipboard', clipHtml);
    var ta = document.getElementById('clipText');
    if (lastClipText) ta.value = lastClipText;
    ta.oninput = function () {
        lastClipText = ta.value;
        if (rfb && connected) rfb.clipboardPasteFrom(ta.value);
    };
};

/* ---- settings panel ---- */
document.getElementById('btnToggleSettings').onclick = function () {
    if (activePanel === 'settings') { closePanel(); return; }
    var settingsHtml =
      '<div class="srow"><div><div class="slabel">Scale viewport</div><div class="sdesc">Fit remote to screen</div></div><div class="stoggle on" id="togScale"><div class="knob"></div></div></div>' +
      '<div class="srow"><div><div class="slabel">Clip viewport</div><div class="sdesc">Show scrollbars when scaled</div></div><div class="stoggle on" id="togClip"><div class="knob"></div></div></div>' +
      '<div class="srow"><div><div class="slabel">Resize session</div><div class="sdesc">Remote resizes with window</div></div><div class="stoggle" id="togResize"><div class="knob"></div></div></div>';
    openPanel('settings', 'Settings', settingsHtml);
    setupToggle('togScale', function (on) { if (rfb) rfb.scaleViewport = on; });
    setupToggle('togClip', function (on) { if (rfb) rfb.clipViewport = on; });
    setupToggle('togResize', function (on) { if (rfb) rfb.resizeSession = on; });
};

function setupToggle(id, callback) {
    var el = document.getElementById(id);
    if (!el) return;
    if (typeof rfb !== 'undefined' && rfb) {
        if (id === 'togScale') el.classList.toggle('on', rfb.scaleViewport);
        if (id === 'togClip') el.classList.toggle('on', rfb.clipViewport);
        if (id === 'togResize') el.classList.toggle('on', rfb.resizeSession);
    }
    el.onclick = function () {
        el.classList.toggle('on');
        callback(el.classList.contains('on'));
    };
}

/* ---- info panel ---- */
document.getElementById('btnToggleInfo').onclick = function () {
    if (activePanel === 'info') { closePanel(); return; }
    var infoHtml =
      '<div class="irow"><span class="ilabel">Status</span><span class="ivalue" id="infoStatus">Disconnected</span></div>' +
      '<div class="irow"><span class="ilabel">VM</span><span class="ivalue">' + '%s' + '</span></div>' +
      '<div class="irow"><span class="ilabel">Server</span><span class="ivalue">' + host + ':' + port + '</span></div>' +
      '<div class="irow"><span class="ilabel">Protocol</span><span class="ivalue">RFB 003.008</span></div>' +
      '<div class="irow"><span class="ilabel">Screen</span><span class="ivalue" id="infoRes">Waiting...</span></div>';
    openPanel('info', 'Connection Info', infoHtml);
    if (connected && rfb) {
        document.getElementById('infoStatus').textContent = 'Connected';
        if (rfb._fb_width && rfb._fb_height)
            document.getElementById('infoRes').textContent = rfb._fb_width + 'x' + rfb._fb_height;
    }
};

/* ---- close panel ---- */
document.getElementById('panelClose').onclick = closePanel;

/* ---- connect ---- */
function connect() {
    if (rfb) { try { rfb.disconnect(); } catch(e) {} rfb = null; }
    updateStatus('connecting', 'Connecting');
    rfb = new RFB(document.getElementById('screen'), url, {
        wsProtocols: ['binary'], repeaterID: '', shared: true, credentials: { password: '' }
    });
    rfb.addEventListener('connect', function () {
        connected = true;
        updateStatus('ok', 'Connected');
        document.getElementById('btnReconnect').style.display = 'none';
        if (activePanel === 'info') {
            document.getElementById('infoStatus').textContent = 'Connected';
            if (rfb._fb_width && rfb._fb_height)
                document.getElementById('infoRes').textContent = rfb._fb_width + 'x' + rfb._fb_height;
        }
    });
    rfb.addEventListener('disconnect', function (e) {
        connected = false;
        updateStatus('error', 'Disconnected');
        document.getElementById('btnReconnect').style.display = '';
        if (activePanel === 'info') document.getElementById('infoStatus').textContent = 'Disconnected';
    });
    rfb.addEventListener('clipboard', function (e) { syncClipToLocal(e.detail.text); });    rfb.scaleViewport = true;
    rfb.resizeSession = false;
    rfb.clipViewport = true;
}

document.getElementById('btnReconnect').onclick = function () {
    this.style.display = 'none';
    connect();
};

connect();

/* ---- clipboard sync on focus ---- */
window.addEventListener('focus', function () {
    setTimeout(syncClipToRemote, 300);
});

/* ---- sidebar auto-hide ---- */
var sidebarTimer;
document.addEventListener('mousemove', function (e) {
    if (e.clientX < 80 || activePanel) {
        sidebar.classList.add('visible');
        clearTimeout(sidebarTimer);
    } else {
        sidebar.classList.add('visible');
        clearTimeout(sidebarTimer);
        sidebarTimer = setTimeout(function () {
            if (!activePanel) sidebar.classList.remove('visible');
        }, 2000);
    }
});
sidebar.addEventListener('mouseenter', function () { clearTimeout(sidebarTimer); });
sidebar.addEventListener('mouseleave', function () {
    if (!activePanel) {
        sidebarTimer = setTimeout(function () { sidebar.classList.remove('visible'); }, 1000);
    }
});

function toggleFullscreen() {
    if (!document.fullscreenElement) document.documentElement.requestFullscreen();
    else document.exitFullscreen();
}
document.getElementById('btnFullscreen').onclick = toggleFullscreen;
document.addEventListener('keydown', function (e) {
    if (e.key === 'F11') { e.preventDefault(); toggleFullscreen(); }
});
</script>
</body>
</html>`, id, id, id)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

func (h *Handler) DownloadRDP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	vm, err := h.lv.GetDomain(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	ip := h.lv.GetDomainIP(id)
	if ip == "" {
		ip = h.serverIP()
	}

	rdpContent := fmt.Sprintf(`full address:s:%s:3389
prompt for credentials:i:1
administrative session:i:1
`, ip)

	w.Header().Set("Content-Type", "application/x-rdp")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.rdp\"", vm.Name))
	w.Write([]byte(rdpContent))
}

func (h *Handler) DownloadSPICE(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	info, err := h.lv.GetVNCInfo(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	spiceContent := fmt.Sprintf(`[virt-viewer]
type=%s
host=%s
port=%d
delete-this-file=1
full-screen=0
`, info.Type, h.serverIP(), info.Port)

	w.Header().Set("Content-Type", "application/x-virt-viewer")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.vv\"", id))
	w.Write([]byte(spiceContent))
}
