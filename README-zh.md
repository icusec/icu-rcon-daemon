# ICURconDaemon

一个轻量级守护进程，将 RCON 协议转为 HTTP 接口，让你可以通过 REST API 发送 RCON 命令。

## 特性

- 将 RCON 协议转为 HTTP REST API
- 首次运行交互式配置（端口和认证密钥）
- 配置保存到 `config.ini`，后续启动自动读取
- 自动生成 UUID 作为认证密钥（也可自定义）
- 多语言支持（自动检测系统语言：中文/英文）
- 跨平台：Windows 和 Linux

## 快速开始

1. 从 [Releases](../../releases) 下载最新版本
2. 首次运行：

```bash
# Linux
chmod +x ICURconDaemon-linux-amd64
./ICURconDaemon-linux-amd64

# Windows
ICURconDaemon-windows-amd64.exe
```

3. 首次运行时会提示配置：

```
=== ICURconDaemon 首次运行配置 ===
按回车键使用默认值。
端口 [8080]:
AuthKey [随机UUID]:
```

- **端口**：默认 `8080`，按回车使用默认值，或输入自定义端口。
- **AuthKey**：默认生成随机 UUID，按回车使用默认值，或输入自定义密钥。

4. 配置保存到 `config.ini`，后续启动直接读取。

## config.ini

```ini
# ICURconDaemon Config
port=8080
authkey=你的认证密钥
```

## API

### POST `/api/icu/daemon/rcon`

请求体：

```json
{
  "ip": "127.0.0.1",
  "port": 25575,
  "passwd": "rcon密码",
  "msg": "say Hello World",
  "auth": "你的认证密钥"
}
```

响应：

```json
{
  "status": 1,
  "msg": "命令执行成功"
}
```

- `status`：`1` = 成功，`0` = 失败
- `auth`：必须与 `config.ini` 中的 `authkey` 一致

## 从源码构建

需要 Go 1.21+

```bash
go build -ldflags "-s -w -X main.Version=v1.0.0" -o ICURconDaemon .
```

## 版本号

当前版本号定义在 `version.go` 中。发布构建时通过 `-ldflags` 覆盖版本号。

## 许可证

MIT
