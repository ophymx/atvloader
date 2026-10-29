#!/bin/sh
set -e
if ! getent group atvloader >/dev/null; then
    groupadd --system atvloader
fi
if ! getent passwd atvloader >/dev/null; then
    useradd --system --gid atvloader --home-dir /var/lib/atvloader --shell /usr/sbin/nologin \
        --comment "atvloader service" atvloader
fi
