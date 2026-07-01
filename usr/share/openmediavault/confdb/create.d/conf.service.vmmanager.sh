#!/bin/sh
set -e
. /etc/default/openmediavault
. /usr/share/openmediavault/scripts/helper-functions

xpath=/config/services/vmmanager

if ! omv_config_exists "$xpath"; then
    omv_config_add_node /config/services vmmanager
    omv_config_add_key "$xpath" enable "1"
    omv_config_add_key "$xpath" httpsMode "managed"
    omv_config_add_key "$xpath" httpsPort "8443"
    omv_config_add_key "$xpath" sslcertificateref ""
    omv_config_add_key "$xpath" dataDir "/opt/openmediavault/vmmanager"
    omv_config_add_key "$xpath" port "8080"
    omv_config_add_key "$xpath" bind "127.0.0.1"
fi

# Upgrade-tolerant: add missing keys on upgrades.
for key in httpsMode httpsPort sslcertificateref dataDir port bind; do
    if ! omv_config_exists "$xpath/$key"; then
        case "$key" in
            httpsMode)         omv_config_add_key "$xpath" "httpsMode" "managed" ;;
            httpsPort)         omv_config_add_key "$xpath" "httpsPort" "8443" ;;
            sslcertificateref) omv_config_add_key "$xpath" "sslcertificateref" "" ;;
            dataDir)           omv_config_add_key "$xpath" "dataDir" "/opt/openmediavault/vmmanager" ;;
            port)              omv_config_add_key "$xpath" "port" "8080" ;;
            bind)              omv_config_add_key "$xpath" "bind" "127.0.0.1" ;;
        esac
    fi
done

# Clean up old properties from pre-rewrite versions.
for key in https certFile keyFile caddyPort extraArgs; do
    if omv_config_exists "$xpath/$key"; then
        omv_config_delete_key "$xpath/$key" 2>/dev/null || true
    fi
done

exit 0
