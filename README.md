# Local Monitor

A Go tool to monitor if certain devices are online in your LAN by sending ARP queries. Device status is stored in a SQLite database for historical tracking.

## Features

- **Pure Go implementation**: No CGO required, cross-platform compatibility
- **ARP-based monitoring**: Uses ARP requests to detect device presence on the local network
- **SQLite storage**: Persists device status history with timestamps (using modernc.org/sqlite)
- **Configurable**: YAML configuration for intervals, timeouts, devices, and more
- **CLI interface**: Command-line flags for different operational modes
- **JSON output**: Optional JSON format for scripting and integration
- **Graceful shutdown**: Handles SIGINT/SIGTERM signals properly
- **Retry logic**: Configurable retry attempts with delays
- **Device grouping**: Organize devices by groups (network, storage, etc.)

## Installation

```bash
# Build with pure Go (no CGO required)
CGO_ENABLED=0 go build -o local-monitor ./cmd/local-monitor
```

## Usage

### Configuration

Create a `config.yaml` file (see `config.yaml` for example):

```yaml
database:
  path: "local-monitor.db"

monitor:
  interval: 60s          # How often to check devices
  timeout: 2s            # ARP request timeout
  interface: "eth0"      # Network interface to use
  workers: 10            # Concurrent ARP workers
  retry_count: 3         # Retry attempts per device
  retry_delay: 1s        # Delay between retries
  startup_probe: true    # Run initial probe on startup
  startup_delay: 5s      # Delay before first probe

logging:
  level: "info"          # debug, info, warn, error
  format: "text"         # text, json

devices:
  - name: "Router"
    ip: "192.168.1.1"
    mac: "aa:bb:cc:dd:ee:f1"  # Optional
    group: "network"
  
  - name: "NAS"
    ip: "192.168.1.100"
    group: "storage"
```

### Command-Line Flags

```
-config string    Path to configuration file (default "config.yaml")
-version          Show version information
-status           Show current device statuses
-probe            Run a single probe and exit
-json             Output in JSON format
-cleanup int      Cleanup records older than N days (0 = disabled)
```

### Examples

**Start continuous monitoring:**
```bash
sudo ./local-monitor -config config.yaml
```

**Run a single probe:**
```bash
sudo ./local-monitor -probe -json
```

**View current status:**
```bash
./local-monitor -status
```

**Cleanup old records (older than 30 days):**
```bash
./local-monitor -cleanup 30
```

**JSON output for scripting:**
```bash
./local-monitor -status -json | jq '.[] | select(.Online == true)'
```

## Requirements

- **Root/Administrator privileges**: ARP operations require raw socket access
- **Go 1.21+**: For building from source
- **Network interface**: Must specify a valid network interface in config

## Database Schema

The tool creates two tables:

- **devices**: Stores device configuration (name, IP, MAC, group)
- **device_transitions**: Stores online/offline status changes with timestamps

## Output Formats

### Table Format (default)
```
NAME                 IP              MAC                  STATUS   LAST CHANGED
-------------------------------------------------------------------------------------
Router               192.168.1.1     aa:bb:cc:dd:ee:f1   online   2026-05-30 10:30:00
NAS                  192.168.1.100   N/A                  offline  2026-05-30 10:30:00
```

### JSON Format
```json
[
  {
    "ID": 1,
    "Name": "Router",
    "IP": "192.168.1.1",
    "MAC": "aa:bb:cc:dd:ee:f1",
    "Group": "network",
    "Online": true,
    "LastSeen": "2026-05-30T10:30:00Z",
    "ChangedAt": "2026-05-30T10:30:00Z"
  }
]
```

## Troubleshooting

**"permission denied" errors:**
- Run with `sudo` or as root
- ARP requires raw socket privileges

**"interface not found" errors:**
- Check available interfaces: `ip link` or `ifconfig`
- Update `interface` in config.yaml

**No devices showing as online:**
- Verify IP addresses are correct
- Check if devices are actually on the same subnet
- Ensure firewall isn't blocking ARP responses

## License

MIT
