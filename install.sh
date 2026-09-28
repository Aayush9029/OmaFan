#!/usr/bin/env bash
# Build OmaFan from this checkout and install the service and Omarchy widget.
#   ./install.sh              install or update
#   ./install.sh --uninstall  remove everything and return the fans to firmware
set -euo pipefail

BIN=/usr/local/bin/omafan
UNIT=/etc/systemd/system/omafan.service
PLUGIN_ROOT="${HOME}/.config/omarchy/plugins/local.omafan"
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  printf 'OmaFan: %s\n' "$1" >&2
  exit 1
}

[[ $EUID -ne 0 ]] || fail "run as your user; the script asks for sudo when it needs it"

if [[ ${1-} == --uninstall ]]; then
  # Stopping the service returns the fans to firmware control.
  sudo systemctl disable --now omafan.service 2>/dev/null || true
  sudo rm -f "$UNIT" "$BIN"
  sudo rm -rf /var/lib/omafan
  sudo systemctl daemon-reload
  if command -v omarchy >/dev/null && [[ -d $PLUGIN_ROOT ]]; then
    omarchy plugin remove local.omafan --yes >/dev/null 2>&1 || rm -rf "$PLUGIN_ROOT"
  fi
  printf 'OmaFan removed. The firmware controls the fans again.\n'
  exit 0
fi

[[ "$(uname -s)" == Linux ]] || fail "Linux is required"
[[ "$(cat /sys/class/dmi/id/sys_vendor 2>/dev/null)" == Framework ]] || fail "this is not a Framework computer"
[[ -e /dev/cros_ec ]] || fail "/dev/cros_ec is missing; the cros_ec_lpcs and cros_ec_chardev modules are required"
command -v go >/dev/null || fail "Go is required to build OmaFan (sudo pacman -S go)"
command -v systemctl >/dev/null || fail "systemd is required"

build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
printf 'Building OmaFan\n'
(cd "$project_dir" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$build_dir/omafan" .)

sudo install -Dm755 "$build_dir/omafan" "$BIN"
sudo install -Dm644 "$project_dir/packaging/systemd/omafan.service" "$UNIT"
sudo systemctl daemon-reload
sudo systemctl enable omafan.service >/dev/null
sudo systemctl restart omafan.service

if command -v omarchy >/dev/null && command -v omarchy-shell >/dev/null; then
  mkdir -p "$PLUGIN_ROOT"
  for file in manifest.json Panel.qml CurveEditor.qml Model.js; do
    install -m644 "$project_dir/omarchy/local.omafan/$file" "$PLUGIN_ROOT/$file"
  done
  omarchy plugin validate "$PLUGIN_ROOT" >/dev/null
  timeout 10s omarchy-shell shell rescanPlugins >/dev/null 2>&1 || true
  omarchy plugin enable local.omafan --section right --before omarchy.monitor >/dev/null 2>&1 || true
  printf 'Omarchy widget installed\n'
fi

printf 'OmaFan is running. The firmware keeps control until you turn it on:\n  omafan enable\n'
