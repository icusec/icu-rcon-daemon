# ICURconDaemon

A lightweight **Minecraft / Source RCON server toolkit** with two components:

| Component | Description |
|---|---|
| **ICURconDaemon** | A daemon that exposes the RCON protocol over HTTP REST API |
| **ICU RCON Client** | A native Go GUI desktop client that manages game servers through the daemon API (or direct RCON), shipped as a single executable |

Both are **single-file Go binaries** with no runtime dependencies.

> 📖 **New here? Read the [illustrated tutorial](docs/使用教程.md) (Chinese)** — what this project protects you from, deploying the daemon, claiming a server in the ICU server navigator, and connecting with the desktop client.

---

## Table of Contents

- [ICURconDaemon (daemon)](#icurcondaemon-daemon)
- [ICU RCON Client (desktop client)](#icu-rcon-client-desktop-client)
- [Illustrated tutorial](docs/使用教程.md) (Chinese)
- [v1.1.0 Improvements](#v110-improvements)
- [Development](#development)
- [License](#license)

---

## ICURconDaemon (daemon)

### Features

- Converts RCON protocol to HTTP REST API (single endpoint, zero setup)
- First-run interactive configuration (port & auth key), saved to `config.ini`
- Auto-generated UUID v4 auth key (or custom)
- Multi-language support (auto-detects system locale: zh/en, **including Windows UI language**)
- Cross-platform: Windows & Linux, cross-compilable with `CGO_ENABLED=0`
- **Zero third-party dependencies** since v1.1.0 (Go standard library only)

### Quick Start

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

- **Port**: default `8080`; validated to be within 1–65535.
- **AuthKey**: default is a random UUID.

4. Config is saved to `config.ini` and loaded automatically afterwards. Press `Ctrl+C` for a graceful shutdown.

### config.ini

```ini
# ICURconDaemon Config
port=8080
authkey=your-auth-key-here
```

> A config with a missing port or an empty auth key is treated as unconfigured and the first-run wizard starts again — an empty key would accept every request.

### HTTP API

The API is **unchanged since v1.0.0**.

#### `POST /api/icu/daemon/rcon`

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

- `status`: `1` = success, `0` = failure (see `msg`)
- `auth`: must match the `authkey` in `config.ini`
- Non-POST requests get `405`, malformed bodies get `400`
- The server enforces read/write timeouts and a 64 KB body limit — safe for trusted networks

```bash
curl -X POST http://127.0.0.1:8080/api/icu/daemon/rcon \
  -H "Content-Type: application/json" \
  -d '{"ip":"127.0.0.1","port":25575,"passwd":"rcon-password","msg":"list","auth":"your-auth-key"}'
```

### Build from Source

Requires Go 1.21+, no third-party modules:

```bash
go build -trimpath -ldflags "-s -w -X main.Version=v1.1.0" -o ICURconDaemon .
```

---

## ICU RCON Client (desktop client)

A **native Go GUI client** built with [Gio UI](https://gioui.org) — no Electron, no browser engine, no CGO. Bundles a CJK font and runs as a single file.

### Features

- **Server list** with card UI: add / edit / delete / view details
- **Two connection modes**:
  - **Daemon** (default): talks to this project's `POST /api/icu/daemon/rcon` endpoint
  - **Direct RCON**: bypasses the daemon and speaks Source RCON to the game server
- **Quick start**: everything is stored in `servers.json` next to the executable; the last selection is restored on launch, double-click a card to test the connection
- **RCON panel**:
  - Command input (Enter to send, `↑`/`▼` buttons or arrow keys for history)
  - Per-server command history (last 200 commands, click to reuse)
  - Live output with timestamps and round-trip milliseconds; four color-coded line kinds (command / response / error / system)
  - Click any output line to copy it; "Copy All" and "Clear" included
  - **Force stop**: while a command is in flight the Send button turns into a red Stop button that aborts the request
  - **Connection test** sends `help`, which virtually every RCON implementation supports with no side effects
- **Light / dark theme**: follows the system color mode automatically, with a manual toggle in the footer (choice is remembered)
- **Error handling**: connection failures, auth failures, timeouts, invalid ports and corrupted JSON all surface as clear UI messages; a corrupted `servers.json` is backed up and recreated
- **Bilingual UI**: follows system language, one-click toggle
- **Cross-platform**: Windows & Linux binaries ship with releases (the Linux build targets glibc ≥ 2.35); macOS builds natively on the target OS

### Install & Launch

Download from [Releases](../../releases):

| File | Platform |
|---|---|
| `ICURconClient-windows-amd64.exe` | Windows 10/11 x64 (built with `-H=windowsgui`, no console window) |
| `ICURconClient-linux-amd64` | Linux x64 (glibc ≥ 2.35: Ubuntu 22.04+, Debian 12+; desktop GL environment required) |
| macOS | Build locally on the target OS (Gio's GL backend needs CGO) — see [Development](#development) |

No installation required. On first launch a `servers.json` is created next to the executable.

### Usage Tutorial

1. **Add a server** — click **＋ Add Server**:
   - Name: anything you like
   - Mode **Daemon**: fill in the host/port of the machine running ICURconDaemon plus its AuthKey
   - Mode **Direct RCON**: skip the daemon fields
   - RCON info: game server host, RCON port (Minecraft default 25575, enable `enable-rcon=true` in `server.properties`), RCON password
2. **Test connection** — select the server and click **Test Connection**; the top bar turns green with round-trip time, failures are printed in red with the exact cause.
3. **Run commands** — type into the bottom input (e.g. `list`, `say hello`), press **Enter**. One in-flight request per server keeps responses ordered; every command is recorded in history.
4. **Manage servers** — single-click selects, double-click selects + tests; use the **Details / Edit / Delete** buttons below the list (delete asks for confirmation).
5. **Language** — toggle in the bottom-left corner.

### servers.json

Stored next to the executable:

```json
{
  "version": 1,
  "lastSelected": "a3f2...",
  "servers": [
    {
      "id": "a3f2...",
      "name": "Survival",
      "mode": "daemon",
      "daemonHost": "127.0.0.1",
      "daemonPort": 8080,
      "authKey": "uuid",
      "rconHost": "10.0.0.5",
      "rconPort": 25575,
      "rconPass": "secret",
      "note": "main",
      "history": ["list"],
      "createdAt": "2025-01-01T12:00:00Z"
    }
  ]
}
```

- Saves are atomic (temp file + rename); a crash cannot truncate the file
- The file contains secrets — do not commit or share it
- A corrupted file is renamed to `servers.json.corrupt-<timestamp>` and the client continues with an empty list

### Troubleshooting

| Symptom | Cause & fix |
|---|---|
| `connection refused` (daemon mode) | Game server RCON not enabled or wrong host/port; check `enable-rcon`, `rcon.port` |
| `Invalid authentication key` | Client AuthKey differs from the daemon's `config.ini` |
| `RCON authentication failed` (direct mode) | Wrong RCON password |
| `dial tcp ... i/o timeout` | Network/firewall or target not running |
| `认证时返回了不可用的数据` / `unexpected response format` | Target is not a standard Source RCON server, or the port is wrong |

---

## v1.1.0 Improvements

With the **HTTP API and `config.ini` format fully preserved**:

- **Zero third-party dependencies** (removed `gorilla/mux` and `google/uuid`)
- **Fixed an RCON connection leak**: failed handshakes now always close the socket (regression-tested)
- **Rewritten RCON packet IO**: exact `io.ReadFull` reads instead of `bytes.Buffer` + reflection-based `binary.Read`; one allocation per command; fixed a potential out-of-range panic for oversized packets
- **Hardened HTTP server**: full timeout set, 64 KB body cap, method allow-list, graceful shutdown
- **Robust config**: port validation, empty auth key rejected, CRLF tolerated
- **Windows Chinese detection** via `GetUserDefaultUILanguage`
- **Observability**: per-command latency logging; failed auths logged with source IP

---

## Development

```bash
# Daemon: build + full test suite (protocol unit tests + HTTP end-to-end)
go build ./... && go test ./... -count=1

# Client: build (single file)
cd client
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ../ICURconClient.exe .
./ICURconClient --selftest
```

Client build requires Go 1.24+. The **Windows** client builds with `CGO_ENABLED=0` (single file). **Linux / macOS** clients must be built on the target OS because Gio's GL backend uses CGO:

```bash
# Debian/Ubuntu dev deps
sudo apt install libgl1-mesa-dev xorg-dev
cd client
go build -trimpath -ldflags "-s -w -X main.clientVersion=v1.1.0" -o ../ICURconClient .
```

## License

- Project code: [MIT](LICENSE)
- Bundled font [Noto Sans SC](https://fonts.google.com/noto/specimen/Noto+Sans+SC): [SIL Open Font License 1.1](client/assets/fonts/OFL.txt)
