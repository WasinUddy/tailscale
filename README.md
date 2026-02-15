# Tailscale

https://tailscale.com

Private WireGuard® networks made easy

## 🔧 Custom Fork Modifications

This fork includes custom modifications to the tailscaled daemon:

### **Embedded Web Server (Port 6942)**

A custom web server has been added to `tailscaled` that provides system monitoring and management capabilities, accessible only from within your Tailscale network.

**Features:**
- **`GET /`** - Returns the hostname of the machine
- **`GET /metrics`** - Exports system metrics in Prometheus format:
  - CPU usage percentage
  - Memory usage (bytes and percentage)
  - Disk usage (bytes and percentage)  
  - Network statistics (bytes sent/received)
  - System uptime in seconds
- **`POST /shutdown`** - Graceful or forced system shutdown
  - `?force=false` (default) - Graceful shutdown with 1-minute delay
  - `?force=true` - Immediate forced shutdown

**Security:**
- 🔒 Only accessible from Tailscale network (100.64.0.0/10) or localhost
- Requests from non-Tailscale IPs are blocked with HTTP 403
- All endpoints validate source IP on every request

**Cross-platform support:**
- ✅ macOS - Uses system commands and syscalls
- ✅ Linux - Reads from /proc filesystem
- ✅ Windows - Uses Win32 APIs

**Usage:**
```bash
# From localhost
curl http://localhost:6942/metrics

# From another Tailscale device
curl http://[tailscale-ip]:6942/metrics

# Graceful shutdown (1-minute delay)
curl -X POST http://[tailscale-ip]:6942/shutdown

# Force immediate shutdown
curl -X POST 'http://[tailscale-ip]:6942/shutdown?force=true'
```

**Implementation:**
- Code: `cmd/tailscaled/web/`
- Documentation: `cmd/tailscaled/web/README.md`
- Clean architecture with platform-specific implementations

**Building this fork:**
```bash
go install tailscale.com/cmd/tailscaled
```

The web server automatically starts when tailscaled launches and binds to port 6942.

---

## Overview

This repository contains the majority of Tailscale's open source code.
Notably, it includes the `tailscaled` daemon and
the `tailscale` CLI tool. The `tailscaled` daemon runs on Linux, Windows,
[macOS](https://tailscale.com/kb/1065/macos-variants/), and to varying degrees
on FreeBSD and OpenBSD. The Tailscale iOS and Android apps use this repo's
code, but this repo doesn't contain the mobile GUI code.

Other [Tailscale repos](https://github.com/orgs/tailscale/repositories) of note:

* the Android app is at https://github.com/tailscale/tailscale-android
* the Synology package is at https://github.com/tailscale/tailscale-synology
* the QNAP package is at https://github.com/tailscale/tailscale-qpkg
* the Chocolatey packaging is at https://github.com/tailscale/tailscale-chocolatey

For background on which parts of Tailscale are open source and why,
see [https://tailscale.com/opensource/](https://tailscale.com/opensource/).

## Using

We serve packages for a variety of distros and platforms at
[https://pkgs.tailscale.com](https://pkgs.tailscale.com/).

## Other clients

The [macOS, iOS, and Windows clients](https://tailscale.com/download)
use the code in this repository but additionally include small GUI
wrappers. The GUI wrappers on non-open source platforms are themselves
not open source.

## Building

We always require the latest Go release, currently Go 1.25. (While we build
releases with our [Go fork](https://github.com/tailscale/go/), its use is not
required.)

```
go install tailscale.com/cmd/tailscale{,d}
```

If you're packaging Tailscale for distribution, use `build_dist.sh`
instead, to burn commit IDs and version info into the binaries:

```
./build_dist.sh tailscale.com/cmd/tailscale
./build_dist.sh tailscale.com/cmd/tailscaled
```

If your distro has conventions that preclude the use of
`build_dist.sh`, please do the equivalent of what it does in your
distro's way, so that bug reports contain useful version information.

## Bugs

Please file any issues about this code or the hosted service on
[the issue tracker](https://github.com/tailscale/tailscale/issues).

## Contributing

PRs welcome! But please file bugs. Commit messages should [reference
bugs](https://docs.github.com/en/github/writing-on-github/autolinked-references-and-urls).

We require [Developer Certificate of
Origin](https://en.wikipedia.org/wiki/Developer_Certificate_of_Origin)
`Signed-off-by` lines in commits.

See [commit-messages.md](docs/commit-messages.md) (or skim `git log`) for our commit message style.

## About Us

[Tailscale](https://tailscale.com/) is primarily developed by the
people at https://github.com/orgs/tailscale/people. For other contributors,
see:

* https://github.com/tailscale/tailscale/graphs/contributors
* https://github.com/tailscale/tailscale-android/graphs/contributors

## Legal

WireGuard is a registered trademark of Jason A. Donenfeld.
