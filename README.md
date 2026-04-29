# AMG-RFID Gateway

Edge Gateway for Raspberry Pi that connects RFID antennas to the cloud backend.

## Overview

This gateway acts as a bridge between RFID antennas (using raw TCP protocol) and the AMG-RFID cloud backend. It provides:

- **Local Caching**: SQLite-based buffering for offline operation
- **Automatic Sync**: Batches readings to the cloud via secure WebSocket/HTTP
- **Multi-Antenna Support**: Connect multiple antennas simultaneously
- **TUI Configurator**: Terminal-based configuration interface
- **OTA Updates**: Automatic over-the-air updates from GitHub releases
- **Health Monitoring**: Built-in health checks and Prometheus metrics

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Raspberry Pi (Gateway)                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │  Antenna 1   │  │  Antenna 2   │  │   TUI Config     │   │
│  │  (Raw TCP)   │  │  (Raw TCP)   │  │   (Bubbletea)    │   │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘   │
│         │                 │                    │             │
│         └─────────────────┼────────────────────┘             │
│                           ▼                                  │
│                  ┌────────────────┐                          │
│                  │  SQLite Cache  │                          │
│                  │  (local store) │                          │
│                  └───────┬────────┘                          │
│                          │                                   │
│                          ▼                                   │
│                  ┌────────────────┐                          │
│                  │  Sync Engine   │                          │
│                  │  (batch+retry) │                          │
│                  └───────┬────────┘                          │
│                          │                                   │
└──────────────────────────┼───────────────────────────────────┘
                           │ HTTPS/WSS
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    Cloud Backend                             │
│              api.amg-rfid.com/api/v1/rfid/sync              │
└─────────────────────────────────────────────────────────────┘
```

## Features

### Core Features

| Feature | Description |
|---------|-------------|
| **SQLite Cache** | Thread-safe local storage with WAL mode for durability |
| **Buffered Writes** | 1000-capacity channel with background flush every 1s |
| **Batch Sync** | Sends up to 100 readings per request to minimize API calls |
| **Auto-Retry** | Exponential backoff (max 5 retries) for failed syncs |
| **Reconnect Logic** | Automatic WebSocket reconnection with backoff |
| **Multi-Antenna** | Supports multiple antennas via goroutines |

### TUI Configurator

Interactive terminal interface using Bubbletea:

```bash
# Run TUI configurator
./gateway --tui

# Or use the dedicated binary
./tui-configurator
```

**Screens:**
- Main Menu: Antennas, Network, System Status, Quit
- Antennas: View all connected antennas and their status
- Network: Connection state to cloud backend
- System: Metrics, cache size, sync stats

### OTA Updates

Automatic updates from GitHub releases:

```bash
# Check for updates manually
./gateway --check-update

# Update via script
sudo ./scripts/update.sh
```

**Safety Features:**
- SHA256 checksum verification
- Automatic backup before update
- Health check after update
- Automatic rollback on failure
- Preserves config.yaml and data/

## Installation

### Quick Install (Recommended)

```bash
# Download and run install script
curl -sSL https://raw.githubusercontent.com/amg-rfid/amg-rfid-gateway/main/scripts/install.sh | sudo bash

# Or with wget
wget -qO- https://raw.githubusercontent.com/amg-rfid/amg-rfid-gateway/main/scripts/install.sh | sudo bash
```

### Manual Installation

```bash
# 1. Create user and directories
sudo useradd -r -s /bin/false gateway
sudo mkdir -p /opt/amg-rfid-gateway/data
sudo chown -R gateway:gateway /opt/amg-rfid-gateway

# 2. Download binary
wget https://github.com/amg-rfid/amg-rfid-gateway/releases/latest/download/gateway-linux-arm64
sudo mv gateway-linux-arm64 /opt/amg-rfid-gateway/gateway
sudo chmod +x /opt/amg-rfid-gateway/gateway

# 3. Create config
cp configs/config.example.yaml /opt/amg-rfid-gateway/config.yaml
# Edit config.yaml with your settings

# 4. Install systemd service
sudo cp scripts/systemd/gateway.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable gateway
sudo systemctl start gateway
```

### Build from Source

```bash
# Requirements: Go 1.25+

# Clone repository
git clone https://github.com/amg-rfid/amg-rfid-gateway.git
cd amg-rfid-gateway

# Build for Raspberry Pi (ARM64)
make build-arm64

# Or build for ARMv7 (older Pi)
make build-arm

# Build all binaries
make build-all
```

## Configuration

Edit `/opt/amg-rfid-gateway/config.yaml`:

```yaml
gateway:
  id: "gateway-001"              # Unique gateway ID
  company_id: "company-001"      # Your company ID
  jwt_token: "your-jwt-token"    # Authentication token

cloud:
  host: "api.amg-rfid.com"       # Backend hostname
  port: 443                      # HTTPS port
  use_tls: true                  # Enable TLS/WSS
  sync_interval: "30s"           # How often to sync

antennas:
  - id: "antenna-1"
    host: "192.168.1.100"        # Antenna IP address
    port: 10001                  # Antenna TCP port
  - id: "antenna-2"
    host: "192.168.1.101"
    port: 10001

cache:
  data_dir: "/opt/amg-rfid-gateway/data"
  max_size_mb: 100

logging:
  level: "info"                  # debug, info, warn, error
  format: "json"                 # json, text

ota:
  enabled: true
  check_interval: "24h"
  github_repo: "amg-rfid/amg-rfid-gateway"
```

## Usage

### Start Gateway

```bash
# As systemd service
sudo systemctl start gateway
sudo systemctl status gateway

# Manual (for testing)
./gateway --config /opt/amg-rfid-gateway/config.yaml

# With verbose logging
./gateway --config /opt/amg-rfid-gateway/config.yaml --log-level debug
```

### View Logs

```bash
# Via systemd
sudo journalctl -u gateway -f

# Or check log files
sudo tail -f /opt/amg-rfid-gateway/data/gateway.log
```

### TUI Configuration

```bash
# Launch TUI
./tui-configurator

# Navigate with arrow keys
# Enter to select
# q or Ctrl+C to quit
```

### Health Check

```bash
# Check gateway health
curl http://localhost:8080/health

# Response:
# {
#   "status": "healthy",
#   "gateway_id": "gateway-001",
#   "antennas_connected": 2,
#   "cache_pending": 0,
#   "last_sync": "2024-01-15T10:30:00Z"
# }
```

### Prometheus Metrics

```bash
# Scrape metrics
curl http://localhost:8080/metrics

# Key metrics:
# gateway_readings_pending - Number of unsynced readings
# gateway_sync_success_total - Total successful syncs
# gateway_sync_failure_total - Total failed syncs
# gateway_uptime_seconds - Gateway uptime
# gateway_antennas_connected - Number of connected antennas
```

## Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package tests
go test ./internal/cache/... -v
go test ./internal/sync/... -v
go test ./test/e2e/... -v

# E2E tests (require mock backend)
go test ./test/e2e/... -v --tags=e2e
```

## API Integration

The gateway communicates with the cloud backend using these endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/rfid/sync` | POST | Send batch of readings |
| `/api/v1/rfid/pending` | GET | Get pending commands |
| `/api/v1/health/gateways` | GET | Health check |

**Authentication:** JWT Bearer token with `gateway_id` and `company_id` claims.

## Troubleshooting

### Gateway won't start

```bash
# Check logs
sudo journalctl -u gateway -n 50

# Verify config
cat /opt/amg-rfid-gateway/config.yaml

# Check file permissions
ls -la /opt/amg-rfid-gateway/
```

### Antennas not connecting

```bash
# Test antenna connectivity
nc -zv 192.168.1.100 10001

# Check antenna config in TUI
./tui-configurator
# Navigate to Antennas screen
```

### Sync failures

```bash
# Check network connectivity
curl -I https://api.amg-rfid.com/health

# Verify JWT token
curl -H "Authorization: Bearer YOUR_TOKEN" \
  https://api.amg-rfid.com/api/v1/health/gateways

# Check cache status
curl http://localhost:8080/health
```

### Update failures

```bash
# Manual update
sudo ./scripts/update.sh --force

# Check GitHub releases
curl -s https://api.github.com/repos/amg-rfid/amg-rfid-gateway/releases/latest
```

## Development

```bash
# Install dependencies
go mod download

# Run linter
make lint

# Format code
make fmt

# Build locally
make build

# Cross-compile for Pi
GOOS=linux GOARCH=arm64 go build -o gateway-arm64 ./cmd/gateway
```

## Project Structure

```
amg-rfid-gateway/
├── cmd/
│   ├── gateway/          # Main gateway binary
│   └── tui/              # TUI configurator binary
├── internal/
│   ├── cache/            # SQLite cache implementation
│   ├── config/           # Configuration loading
│   ├── health/           # Health monitoring
│   ├── monitoring/       # Prometheus metrics
│   ├── rawtcp/           # Antenna TCP client
│   ├── sync/             # Cloud sync engine
│   ├── tui/              # TUI screens
│   ├── updater/          # OTA update logic
│   └── wsclient/         # WebSocket client
├── configs/
│   └── config.example.yaml
├── scripts/
│   ├── install.sh
│   ├── update.sh
│   └── systemd/
├── test/
│   └── e2e/              # Integration tests
├── Makefile
├── go.mod
└── README.md
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Support

- 📧 Email: support@amg-rfid.com
- 💬 Discord: [AMG-RFID Community](https://discord.gg/amg-rfid)
- 🐛 Issues: [GitHub Issues](https://github.com/amg-rfid/amg-rfid-gateway/issues)

## Roadmap

- [x] Core gateway with SQLite cache
- [x] TUI configurator
- [x] OTA updates
- [x] Prometheus metrics
- [ ] Web-based configuration UI
- [ ] Edge ML for tag filtering
- [ ] Multi-backend support
