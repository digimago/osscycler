#!/usr/bin/env bash
# Godot 4.7.2 (.NET) and its export templates, for exporting the 3D view
# in release builds: downloaded once into $GODOT_DIR (default _godot),
# checked against the release's SHA-512 (godot-builds' SHA512-SUMS.txt),
# the templates installed where Godot looks for them; prints the editor's
# path. Run again, it finds what is there (CI caches both directories).
set -euo pipefail

V=4.7.2
EDITOR=Godot_v${V}-stable_mono_linux_x86_64
EDITOR_SHA512=1855960b27ee3ef5e66e5e228cced69d55637b24334a7411162687dcd077d8f9f645348cdb8eae984bec8135d49ed855a1e3a16476786b8bce60774fd8402d13
TEMPLATES=Godot_v${V}-stable_mono_export_templates.tpz
TEMPLATES_SHA512=bb5c41d72370ed743660361f6228006f808ab04ca33abdc545d740b044f3fe057f32ae8cb7873a1bc86ddcd82ae683b9f6dfdfe4179852f2c0f1acde2ff6bd5a
BASE=https://github.com/godotengine/godot-builds/releases/download/${V}-stable
DIR=${GODOT_DIR:-_godot}
TEMPLATE_DIR=${XDG_DATA_HOME:-$HOME/.local/share}/godot/export_templates/${V}.stable.mono

# fetch NAME SHA512: NAME into $DIR, checked.
fetch() {
	curl -fsSL --retry 3 -o "$DIR/$1.part" "$BASE/$1"
	echo "$2  $DIR/$1.part" | sha512sum -c --quiet - >&2
	mv "$DIR/$1.part" "$DIR/$1"
}

mkdir -p "$DIR"
editor=$DIR/$EDITOR/${EDITOR%_x86_64}.x86_64
if [[ ! -x $editor ]]; then
	fetch "$EDITOR.zip" "$EDITOR_SHA512"
	unzip -q -o "$DIR/$EDITOR.zip" -d "$DIR"
	rm "$DIR/$EDITOR.zip"
fi
if [[ ! -f $TEMPLATE_DIR/version.txt ]]; then
	fetch "$TEMPLATES" "$TEMPLATES_SHA512"
	rm -rf "$DIR/templates"
	unzip -q -o "$DIR/$TEMPLATES" -d "$DIR"
	mkdir -p "$TEMPLATE_DIR"
	mv "$DIR"/templates/* "$TEMPLATE_DIR/"
	rm -rf "$DIR/templates" "$DIR/$TEMPLATES"
fi
[[ -x $editor ]] || { echo "godot.sh: no editor at $editor" >&2; exit 1; }
realpath "$editor"
