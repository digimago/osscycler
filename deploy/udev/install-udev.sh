#!/bin/sh
# Sets up the ANT+ stick for osscycler from the release archive; the .deb
# and .rpm packages do this themselves. From the unpacked folder:
#
#   sudo ./install-udev.sh
#
# It installs the udev rule, creates the ant group, adds you (the user
# who ran sudo) to it, and applies the rule.
set -e
[ "$(id -u)" = 0 ] || { echo "run it with sudo: sudo $0" >&2; exit 1; }
here=$(dirname "$0")
mkdir -p /etc/udev/rules.d
install -m 644 "$here/99-ant-usb.rules" /etc/udev/rules.d/99-ant-usb.rules
getent group ant >/dev/null || groupadd --system ant
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != root ]; then
	usermod -aG ant "$SUDO_USER"
	echo "added $SUDO_USER to the ant group: log out and in again"
fi
if command -v udevadm >/dev/null 2>&1; then
	udevadm control --reload-rules
	udevadm trigger --subsystem-match=tty --subsystem-match=usb || true
fi
echo "done: replug the ANT+ stick"
