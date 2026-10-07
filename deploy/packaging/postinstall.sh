#!/bin/sh
# The udev rule gives the group "ant" the ANT+ stick; make sure it exists,
# apply the rule to a stick that is already plugged in, and restart core
# services that were running before an upgrade.
set -e
getent group ant >/dev/null || groupadd --system ant
if command -v udevadm >/dev/null 2>&1; then
	udevadm control --reload-rules || true
	udevadm trigger --subsystem-match=tty --subsystem-match=usb || true
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl try-restart 'osscycler@*.service' || true
fi
echo "osscycler: for the ANT+ stick, join the ant group: sudo usermod -aG ant \$USER (then log in again)"
echo "osscycler: to run the core from boot as you: sudo systemctl enable --now osscycler@\$USER"
