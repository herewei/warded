# Warded systemd deployment

This directory contains the baseline `systemd` unit for running `warded serve`
as a dedicated system user.

Default layout:

- binary: `/usr/local/bin/warded`
- runtime state: `/var/lib/warded`
- environment file: `/etc/warded/warded.env`
- unit file: `/etc/systemd/system/warded.service`

Recommended flow:

1. Install the CLI.
2. Run activation as the same service user:

```bash
sudo -u warded /usr/local/bin/warded activate --config-dir /var/lib/warded ...
```

3. Reload and enable the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now warded.service
```

Notes:

- `warded serve` stays a foreground process; `systemd` is the process supervisor.
- `ward.json` and CertMagic state live under `/var/lib/warded`.
- `install.sh` may prepare the `warded` user, `/var/lib/warded`, `/etc/warded`,
  and the generated unit file when run as root on Linux.
