#!/bin/sh
# The udev rule and the service unit are gone; the ant group stays (others
# may use it), and so does every rider's ~/osscycler.
if command -v udevadm >/dev/null 2>&1; then
	udevadm control --reload-rules || true
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
fi
