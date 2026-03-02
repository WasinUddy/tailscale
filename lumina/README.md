# Lumina Server Integration

This package provides automatic MAC address registration with the Lumina server for Wake-on-LAN functionality.

## Overview

When `tailscaled` starts up, it automatically registers its MAC address with the configured Lumina server. This enables the Lumina server to wake up the device remotely using Wake-on-LAN magic packets through connected agents.

## Configuration

The integration is configured via environment variables:

### `TS_LUMINA_SERVER`
- **Description**: URL of the Lumina server
- **Default**: `http://lumina-server:3000`
- **Example**: `http://192.168.1.100:3000` or `https://lumina.example.com`

### `TS_LUMINA_ENABLED`
- **Description**: Explicitly enable/disable Lumina registration
- **Default**: Automatically enabled if `TS_LUMINA_SERVER` is set
- **Values**: `true` or `false`

### `TS_LUMINA_HOSTNAME`
- **Description**: Hostname to register with the server
- **Default**: Auto-detected using `os.Hostname()`
- **Example**: `my-laptop`

### `TS_LUMINA_INTERFACE`
- **Description**: Network interface to get MAC address from
- **Default**: Auto-detected (prefers Tailscale interface, then first active interface with IP)
- **Example**: `eth0`, `en0`, `tailscale0`

## How It Works

1. **Startup**: When `tailscaled` successfully starts the LocalBackend, it loads the Lumina configuration
2. **Auto-detection**: If not explicitly configured, it auto-detects hostname and MAC address
3. **Registration**: Sends a POST request to `/api/v1/devices/{hostname}/mac` with the MAC address
4. **Retry Logic**: Retries up to 5 times with exponential backoff (2s, 4s, 8s, 16s, 32s)
5. **Non-blocking**: Runs in a background goroutine to not block daemon startup
6. **Logging**: All operations are logged for debugging

## Usage Examples

### Default Configuration (MagicDNS)
Uses default `lumina-server:3000` which is resolved via Tailscale MagicDNS:

```bash
# No configuration needed if lumina-server is in your tailnet
sudo systemctl start tailscaled
```

### Custom Server URL
```bash
export TS_LUMINA_SERVER="http://192.168.1.100:3000"
sudo tailscaled
```

### Explicit Hostname
Useful when hostname detection doesn't match your Tailscale name:
```bash
export TS_LUMINA_SERVER="http://lumina-server:3000"
export TS_LUMINA_HOSTNAME="my-custom-name"
sudo tailscaled
```

### Specific Network Interface
Force MAC address from a specific interface:
```bash
export TS_LUMINA_SERVER="http://lumina-server:3000"
export TS_LUMINA_INTERFACE="eth0"
sudo tailscaled
```

### Disable Integration
```bash
export TS_LUMINA_ENABLED=false
sudo tailscaled
```

## Systemd Service Configuration

Add environment variables to your systemd service file:

```ini
[Unit]
Description=Tailscale node agent
Documentation=https://tailscale.com/kb/
After=network-pre.target
Wants=network-pre.target

[Service]
EnvironmentFile=-/etc/default/tailscaled
Environment="TS_LUMINA_SERVER=http://lumina-server:3000"
Environment="TS_LUMINA_ENABLED=true"
ExecStart=/usr/sbin/tailscaled
Restart=on-failure
RuntimeDirectory=tailscale
RuntimeDirectoryMode=0755
StateDirectory=tailscale
StateDirectoryMode=0750
CacheDirectory=tailscale
CacheDirectoryMode=0750

[Install]
WantedBy=multi-user.target
```

Or use `/etc/default/tailscaled`:
```bash
TS_LUMINA_SERVER=http://lumina-server:3000
TS_LUMINA_ENABLED=true
```

## Docker Container Configuration

```dockerfile
FROM tailscale/tailscale:latest

ENV TS_LUMINA_SERVER=http://lumina-server:3000
ENV TS_LUMINA_ENABLED=true

CMD ["tailscaled"]
```

Or using docker-compose:
```yaml
services:
  tailscale:
    image: tailscale/tailscale:latest
    environment:
      TS_LUMINA_SERVER: http://lumina-server:3000
      TS_LUMINA_ENABLED: "true"
      TS_LUMINA_HOSTNAME: my-container
```

## API Endpoint

The registration sends a POST request to:
```
POST {TS_LUMINA_SERVER}/api/v1/devices/{hostname}/mac
Content-Type: application/json

{
  "macAddress": "AA:BB:CC:DD:EE:FF"
}
```

## Troubleshooting

### Enable Verbose Logging
Add `--verbose=1` to see Lumina registration logs:
```bash
tailscaled --verbose=1
```

### Check Logs
```bash
# Systemd
sudo journalctl -u tailscaled -f | grep lumina

# Direct run
tailscaled --verbose=1 2>&1 | grep lumina
```

### Common Issues

**Registration disabled**
```
lumina: registration disabled (set TS_LUMINA_ENABLED=true or TS_LUMINA_SERVER to enable)
```
Solution: Set `TS_LUMINA_SERVER` environment variable

**Failed to get MAC address**
```
lumina: failed to register MAC address: failed to get MAC address: no suitable network interface found
```
Solution: Ensure at least one non-loopback interface is UP and has an IP address, or specify `TS_LUMINA_INTERFACE`

**Connection refused**
```
lumina: registration attempt 1/5 failed: failed to send request: dial tcp: connect: connection refused
```
Solution: Verify Lumina server is running and accessible, check firewall rules

**Hostname not found**
```
lumina: registration attempt 1/5 failed: server returned status 404
```
Solution: The device must be registered in Tailscale first; ensure it's authorized in your tailnet

## Integration with Lumina Server

Once registered, the device can be woken up via the Lumina server:

```bash
# Wake by hostname (recommended)
curl -X POST "http://lumina-server:3000/api/v1/wake?target=my-laptop"

# Wake by MAC address (fallback)
curl -X POST "http://lumina-server:3000/api/v1/wake?target=AA:BB:CC:DD:EE:FF"
```

## Security Considerations

- Registration is done over HTTP/HTTPS to the Lumina server
- No authentication is required for registration (assumes trusted network)
- MAC addresses are stored in-memory on the Lumina server (not persisted)
- Registration happens on every daemon startup

For production use, consider:
1. Running Lumina server over HTTPS
2. Restricting access to the Lumina server using Tailscale ACLs
3. Using Tailscale MagicDNS for automatic hostname resolution

## Architecture

```
┌─────────────────┐                 ┌─────────────────┐
│   tailscaled    │                 │ Lumina Server   │
│                 │                 │                 │
│  1. Startup     │                 │  3. Store       │
│  2. Register ──────────HTTP───────▶  hostname →     │
│     MAC         │                 │  MAC mapping    │
│                 │                 │                 │
│                 │                 │  4. Wake API    │
│                 │◀────WebSocket───┤  broadcasts     │
│                 │     (Agent)     │  to agents      │
│  5. Send WoL    │                 │                 │
│     packet      │                 └─────────────────┘
└─────────────────┘
```

## Development

### Building with Lumina Support

The integration is automatically included when building tailscaled:

```bash
cd cmd/tailscaled
go build .
```

### Testing

Test the registration without running full daemon:

```go
package main

import (
	"log"
	"tailscale.com/lumina"
)

func main() {
	cfg := &lumina.Config{
		ServerURL: "http://localhost:3000",
		Hostname:  "test-device",
		Enabled:   true,
	}

	logf := log.Printf
	lumina.RegisterMACAddress(logf, cfg)

	// Wait a bit for async registration
	time.Sleep(5 * time.Second)
}
```

## License

BSD-3-Clause (same as Tailscale)
