# AMG RFID Gateway - Scripts and Deployment

This directory contains installation, update, and deployment scripts for the AMG RFID Gateway.

## Quick Start

```bash
# 1. Download the install script
curl -fsSL https://raw.githubusercontent.com/amg-rfid/amg-rfid-gateway/main/scripts/install.sh -o install.sh
chmod +x install.sh

# 2. Run installation (requires sudo)
sudo ./install.sh

# 3. Edit configuration
sudo nano /opt/amg-rfid-gateway/config.yaml

# 4. Start the service
sudo systemctl start amg-rfid-gateway
```

## Scripts

### install.sh

Installs the AMG RFID Gateway on a Raspberry Pi or ARM64 system.

**Usage:**

```bash
sudo ./install.sh [OPTIONS]
```

**Options:**

- `-v, --version VERSION` - Install specific version (default: latest)
- `-b, --binary PATH` - Install local binary instead of downloading
- `-h, --help` - Show help message

**Examples:**

```bash
# Install latest version
sudo ./install.sh

# Install specific version
sudo ./install.sh --version v1.2.0

# Install local binary
sudo ./install.sh --binary ./gateway-arm64
```

**What it does:**

1. Creates `gateway` user and group
2. Creates directory structure at `/opt/amg-rfid-gateway/`
3. Downloads (or copies) the gateway binary
4. Creates default configuration file
5. Installs systemd service
6. Enables auto-start on boot

### systemd/gateway.service

Systemd service unit file for running the gateway as a system service.

**Features:**

- Auto-start on boot
- Automatic restart on failure
- Graceful shutdown handling
- Resource limits
- Security hardening (sandboxing)
- Structured logging to journald

**Management commands:**

```bash
# Check status
sudo systemctl status amg-rfid-gateway

# Start service
sudo systemctl start amg-rfid-gateway

# Stop service
sudo systemctl stop amg-rfid-gateway

# Restart service
sudo systemctl restart amg-rfid-gateway

# View logs
sudo journalctl -u amg-rfid-gateway -f

# Enable auto-start
sudo systemctl enable amg-rfid-gateway

# Disable auto-start
sudo systemctl disable amg-rfid-gateway
```

## Directory Structure

After installation, the following structure is created:

```
/opt/amg-rfid-gateway/
├── gateway                    # Main binary
├── config.yaml                # Configuration file
├── data/
│   └── readings.db           # SQLite database (auto-created)
├── backup/
│   ├── gateway-v1.0.0-...    # Backup binaries
│   └── gateway-v1.1.0-...    # Previous versions
└── scripts/
    ├── install.sh            # Installation script
    └── systemd/
        └── gateway.service   # Systemd unit file
```

## Configuration

The configuration file is located at `/opt/amg-rfid-gateway/config.yaml`.

See `../configs/config.example.yaml` for a documented example.

**Important:** Never edit the config file during an update - it is preserved automatically.

## Requirements

- Raspberry Pi 4/5 or ARM64 system
- Debian/Ubuntu-based OS (Raspberry Pi OS, Ubuntu Server)
- systemd (system and service manager)
- Network connectivity for downloading updates
- sudo/root access for installation

## Troubleshooting

### Service won't start

```bash
# Check status
sudo systemctl status amg-rfid-gateway

# View logs
sudo journalctl -u amg-rfid-gateway -n 50

# Verify configuration
sudo /opt/amg-rfid-gateway/gateway --config /opt/amg-rfid-gateway/config.yaml --validate
```

### Updating

Updates go through Homebrew (`brew update && brew upgrade amg-rfid-gateway`).
For `/opt` installs, re-run `install.sh` with the new binary; it backs up the
previous binary to `/opt/amg-rfid-gateway/backup/`.

```bash
# Manual rollback of an /opt install
sudo systemctl stop amg-rfid-gateway
sudo cp /opt/amg-rfid-gateway/backup/gateway-backup-TIMESTAMP /opt/amg-rfid-gateway/gateway
sudo systemctl start amg-rfid-gateway
```

### Permission issues

```bash
# Fix ownership
sudo chown -R gateway:gateway /opt/amg-rfid-gateway

# Fix permissions
sudo chmod 750 /opt/amg-rfid-gateway/data /opt/amg-rfid-gateway/backup
sudo chmod 640 /opt/amg-rfid-gateway/config.yaml
```

## Health Check

The gateway exposes a health endpoint:

```bash
# Local health check
curl http://localhost:8080/health

# Expected response
{"status":"healthy","gateway_id":"rpi-gateway-001","antennas_connected":2}
```

## Monitoring

Metrics are available at:

```bash
# Prometheus metrics
curl http://localhost:9090/metrics
```

## Raspberry Pi SSH Access

`setup-raspberry-pi-ssh-access.sh` creates one local SSH alias for a Raspberry Pi.
It is intended to run on the operator workstation, not on the Pi:

```bash
bash scripts/setup-raspberry-pi-ssh-access.sh
# Later, including from an agent:
ssh amg-rfid-pi '<command>'
```

Before running it, create a stable DHCP reservation for the Pi or verify an mDNS/DNS
name such as `amg-rfid-pi.local`. Prefer that hostname over a Wi-Fi/Ethernet IP so
the alias survives address changes. Independently verify the Pi's SSH SHA256
host-key fingerprint through a trusted out-of-band channel; the helper requires that
fingerprint, scans the host key, and only writes a per-alias `known_hosts` file when
the values match exactly. From an already trusted terminal on the Raspberry Pi, run:

```bash
sudo ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256
```

Compare the resulting `SHA256:...` value out of band before entering it into the
helper.

The helper creates or reuses a dedicated Ed25519 identity, refuses symlinked or
unsafe existing key/config files, and keeps unrelated `~/.ssh/config` entries intact.
It never prints the private key. Public-key installation is optional and occurs only
after the fingerprint check, using strict host-key checking. Do not bypass this check
or use `StrictHostKeyChecking=no`.

## Security

The systemd service includes security hardening:

- Runs as non-root user (`gateway`)
- No new privileges
- Read-only filesystem (except data/backup dirs)
- Protected kernel tunables
- Memory protection (no write+execute)

## License

MIT License - See LICENSE file in repository root.
