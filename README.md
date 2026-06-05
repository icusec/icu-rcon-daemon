# ICURconDaemon

A lightweight daemon that exposes the RCON protocol over HTTP, allowing you to send RCON commands via REST API.

## Features

- Convert RCON protocol to HTTP REST API
- First-run interactive configuration (port & auth key)
- Config saved to `config.ini`, auto-load on subsequent starts
- Auto-generated UUID as auth key (or custom)
- Multi-language support (auto-detects system locale: zh/en)
- Cross-platform: Windows & Linux

## Quick Start

1. Download the latest release from [Releases](../../releases)
2. Run the binary for the first time:

```bash
# Linux
chmod +x ICURconDaemon-linux-amd64
./ICURconDaemon-linux-amd64

# Windows
ICURconDaemon-windows-amd64.exe
```

3. On first run, you will be prompted:

```
=== ICURconDaemon First Run Configuration ===
Press Enter to use default value.
Port [8080]:
AuthKey [random UUID]:
```

- **Port**: Default is `8080`. Press Enter to accept, or type a custom port.
- **AuthKey**: Default is a random UUID. Press Enter to accept, or type a custom key.

4. Config is saved to `config.ini`. On subsequent starts, the daemon reads it directly.

## config.ini

```ini
# ICURconDaemon Config
port=8080
authkey=your-auth-key-here
```

## API

### POST `/api/icu/daemon/rcon`

Request body:

```json
{
  "ip": "127.0.0.1",
  "port": 25575,
  "passwd": "rcon-password",
  "msg": "say Hello World",
  "auth": "your-auth-key"
}
```

Response:

```json
{
  "status": 1,
  "msg": "Command executed successfully"
}
```

- `status`: `1` = success, `0` = failure
- `auth`: Must match the `authkey` in `config.ini`

## Build from Source

Requires Go 1.21+

```bash
go build -ldflags "-s -w -X main.Version=v1.0.0" -o ICURconDaemon .
```

## Version

Current version is defined in `version.go`. The build system overrides it via `-ldflags` during release builds.

## License

MIT
