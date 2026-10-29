#!/bin/sh
set -e
chown root:atvloader /etc/atvloader/env
chmod 0640 /etc/atvloader/env
chown atvloader:atvloader /var/lib/atvloader
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable atvloader.service >/dev/null 2>&1 || true
    # Restart on upgrade; a fresh install is started by the operator.
    if systemctl is-active --quiet atvloader.service; then
        systemctl restart atvloader.service
    fi
fi
