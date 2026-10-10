#!/usr/bin/env bash
# THIRD_PARTY_NOTICES.txt for a release (to stdout): the licences of what
# the release ships besides osscycler's own GPL code. The Go modules linked
# into the binaries, from the module cache; with WITH_3D=1 the 3D view's
# Godot engine, .NET runtime, gRPC and Protobuf libraries and font (texts
# kept in deploy/notices, pinned to the versions bundled); and the data
# the ready-built worlds are made from.
set -euo pipefail

cmds=(./cmd/osscycler ./cmd/osscycler-core ./cmd/osscycler-tui ./cmd/osscycler-world)
section() { printf '\n%s\n%s\n\n' "$1" "$(printf '%*s' ${#1} '' | tr ' ' '=')"; }

cat <<'HEAD'
Third-party notices for osscycler

osscycler itself is licensed under the GNU General Public License v3.0
(LICENSE). It includes the software and data below, under their own
licences, whose texts follow.
HEAD

section "Go modules linked into osscycler's programs"
go list -deps -f '{{with .Module}}{{.Path}} {{.Version}} {{.Dir}}{{end}}' "${cmds[@]}" |
	sort -u | grep -v '^github.com/digimago/' | while read -r path version dir; do
	[[ -n $dir ]] || continue
	printf -- '--- %s %s\n\n' "$path" "$version"
	found=
	for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
		[[ -f $f ]] || continue
		cat "$f"
		printf '\n'
		found=1
	done
	[[ -n $found ]] || printf '(no licence file in the module; see https://pkg.go.dev/%s?tab=licenses)\n\n' "$path"
done

if [[ ${WITH_3D:-} == 1 ]]; then
	section "The 3D view (osscycler-3d)"
	printf -- '--- Godot Engine 4.7.2 (MIT)\n\n'
	cat deploy/notices/godot-LICENSE.txt
	printf '\n--- Godot Engine 4.7.2: third-party components\n\n'
	cat deploy/notices/godot-COPYRIGHT.txt
	printf '\n--- .NET runtime 8.0.31 (MIT)\n\n'
	cat deploy/notices/dotnet-runtime-LICENSE.txt
	printf '\n--- .NET runtime 8.0.31: third-party notices\n\n'
	cat deploy/notices/dotnet-runtime-THIRD-PARTY-NOTICES.txt
	printf '\n--- Grpc.Net.Client, Grpc.Net.Common, Grpc.Core.Api 2.84.0 (Apache-2.0)\n\n'
	cat deploy/notices/grpc-dotnet-LICENSE.txt
	printf '\n--- Google.Protobuf 3.36.2 (BSD-3-Clause)\n\n'
	cat deploy/notices/protobuf-LICENSE.txt
	printf '\n--- Atkinson Hyperlegible Next (SIL Open Font License 1.1)\n\n'
	cat renderers/godot/fonts/OFL.txt
fi

section "Data in the ready-built worlds"
cat <<'DATA'
Map data © OpenStreetMap contributors, available under the Open Database
License (ODbL) 1.0: https://www.openstreetmap.org/copyright
Heights: Actueel Hoogtebestand Nederland (AHN), CC0 1.0.
The Posbank Loop route follows OpenStreetMap roads (ODbL).
DATA
