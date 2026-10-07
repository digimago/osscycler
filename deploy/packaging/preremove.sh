#!/bin/sh
# On removal (not on an upgrade) stop the core services; they save the
# ride under way first. dpkg passes "remove", rpm passes 0.
case "$1" in
remove | 0)
	if [ -d /run/systemd/system ]; then
		systemctl stop 'osscycler@*.service' || true
	fi
	;;
esac
