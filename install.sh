#!/usr/bin/env bash
# Build OmaFan from this checkout and install the service and Omarchy widget.
#   ./install.sh              install or update
#   ./install.sh --uninstall  remove everything and return the fans to firmware
set -euo pipefail

BIN=/usr/local/bin/omafan
UNIT=/etc/systemd/system/omafan.service
RULE=/etc/udev/rules.d/60-omafan.rules
PLUGIN_ID=io.github.aayush9029.omafan
PLUGINS="${HOME}/.config/omarchy/plugins"
PLUGIN_ROOT="${PLUGINS}/${PLUGIN_ID}"
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  printf 'OmaFan: %s\n' "$1" >&2
  exit 1
}

have_omarchy() { command -v omarchy >/dev/null && command -v omarchy-shell >/dev/null; }

plugin_state() {
  omarchy plugin list 2>/dev/null | awk -v id="$PLUGIN_ID" '$1 == id { print $2 }'
}

uninstall() {
  # Stopping the service returns the fans to firmware control.
  sudo systemctl disable --now omafan.service 2>/dev/null || true
  sudo systemctl reset-failed omafan.service 2>/dev/null || true
  sudo rm -f "$UNIT" "$RULE" "$BIN"
  sudo rm -rf /var/lib/omafan
  sudo systemctl daemon-reload
  sudo udevadm control --reload 2>/dev/null || true
  for id in "$PLUGIN_ID" local.omafan; do
    [[ -d "$PLUGINS/$id" ]] || continue
    if have_omarchy; then
      omarchy plugin disable "$id" >/dev/null 2>&1 || true
      omarchy plugin remove "$id" --yes >/dev/null 2>&1 || rm -rf "${PLUGINS:?}/$id"
    else
      rm -rf "${PLUGINS:?}/$id"
    fi
  done
  printf 'OmaFan removed. The firmware controls the fans again.\n'
}

[[ $EUID -ne 0 ]] || fail "run as your user; the script asks for sudo when it needs it"

case "${1-}" in
  '') ;;
  --uninstall) uninstall; exit 0 ;;
  *) printf 'Usage: %s [--uninstall]\n' "$0" >&2; exit 2 ;;
esac

[[ "$(uname -s)" == Linux ]] || fail "Linux is required"
[[ "$(cat /sys/class/dmi/id/sys_vendor 2>/dev/null)" == Framework ]] || fail "this is not a Framework computer"
[[ -e /dev/cros_ec ]] || fail "/dev/cros_ec is missing; the cros_ec_lpcs and cros_ec_chardev modules are required"
command -v go >/dev/null || fail "Go is required to build OmaFan (sudo pacman -S go)"
command -v systemctl >/dev/null || fail "systemd is required"

# Check the widget before touching the system, so a bad checkout can't half-install.
if have_omarchy; then
  omarchy plugin validate "$project_dir" >/dev/null || fail "the Omarchy widget in $project_dir is invalid"
fi

build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
printf 'Building OmaFan\n'
(cd "$project_dir" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$build_dir/omafan" .)

sudo install -Dm755 "$build_dir/omafan" "$BIN"
sudo install -Dm644 "$project_dir/packaging/systemd/omafan.service" "$UNIT"
sudo install -Dm644 "$project_dir/packaging/udev/60-omafan.rules" "$RULE"
sudo udevadm control --reload
sudo udevadm trigger --action=add /dev/cros_ec 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable omafan.service >/dev/null
sudo systemctl restart omafan.service

if ! sudo systemctl is-active --quiet omafan.service; then
  sudo journalctl -u omafan.service -n 20 --no-pager >&2 || true
  fail "the service didn't start; see the log above"
fi

if have_omarchy; then
  # Earlier versions installed the widget as local.omafan.
  if [[ -d "$PLUGINS/local.omafan" ]]; then
    omarchy plugin remove local.omafan --yes >/dev/null 2>&1 || rm -rf "$PLUGINS/local.omafan"
  fi
  rm -rf "$PLUGIN_ROOT"
  mkdir -p "$PLUGIN_ROOT/omarchy"
  cp "$project_dir/manifest.json" "$PLUGIN_ROOT/"
  cp "$project_dir"/omarchy/* "$PLUGIN_ROOT/omarchy/"
  omarchy-shell -q shell rescanPlugins
  if [[ "$(plugin_state)" != enabled ]]; then
    omarchy plugin enable "$PLUGIN_ID" --section right --before omarchy.monitor
  fi
  printf 'Omarchy widget installed. If an older version is open, run: omarchy restart shell\n'
fi

printf 'OmaFan is running. The firmware keeps control until you turn it on:\n  omafan enable\n'
