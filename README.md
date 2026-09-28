# OmaFan

Fan profiles and a custom fan curve for the Framework Desktop, right in the Omarchy bar.

![OmaFan panel](assets/omafan-panel.png)

- Live CPU, GPU, and fan readings
- Quiet, Balanced, and Blast profiles, or drag four points to draw your own curve
- One switch hands the fans back to the firmware's own control

## Install

OmaFan needs a Framework Desktop (Ryzen AI Max) with the `cros_ec_lpcs` and `cros_ec_chardev` kernel modules, Go to build it, and sudo to install the service.

```bash
git clone https://github.com/Aayush9029/OmaFan.git
cd OmaFan
./install.sh
```

The installer adds the Omarchy widget when Omarchy is present. The firmware keeps control of the fans until you flip the switch in the panel or run `omafan enable`.

## Use

```text
omafan status [--json]         Temperatures, fan speed, and the active curve
omafan enable                  Let OmaFan drive the fans
omafan disable                 Hand the fans back to the firmware
omafan profile <name>          quiet, balanced, blast, or custom
omafan curve <T:F,...>         Save and use a custom curve, e.g. 40:20,55:35,70:60,85:90
```

Anyone can read the status. Changing settings needs root or a member of the `wheel` group.

## How it works

A small root service, `omafan.service`, reads the hottest CPU temperature (k10temp Tctl or the EC's APU sensor) once a second, looks it up on the active curve, and sets the fan duty through the embedded controller at `/dev/cros_ec`. The fan speeds up at up to 8% a second and slows down at 2% a second, so short spikes don't make it pulse.

Safety comes first:

- At 85°C the fans run at least 70%, and at 90°C they run at full speed, whatever the curve says.
- The fans go back to firmware control when OmaFan is turned off, when the service stops or crashes, when CPU temperatures can't be read for five seconds, or after repeated EC errors.
- A systemd watchdog restarts a stuck service, and every stop runs `omafan restore`.
- The EC resets fans to automatic after suspend; OmaFan reapplies its duty within five seconds.

Settings live in `/var/lib/omafan/settings.json`, and the CLI talks to the service over `/run/omafan/omafan.sock`.

## Troubleshooting

```bash
systemctl status omafan
journalctl -u omafan -b
```

## Remove

```bash
./install.sh --uninstall
```

This stops the service, which returns the fans to the firmware, and removes the service, settings, and widget.

## Development

```bash
go test -race ./...
```

## License

[MIT](LICENSE)
