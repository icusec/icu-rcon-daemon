# ICURconDaemon

[English](README.md) | **中文**

一套轻量的 **Minecraft / 源RCON 服务器管理工具**，由两部分组成：

| 组件 | 说明 |
|---|---|
| **ICURconDaemon** | 守护进程：把 RCON 协议转为 HTTP REST API，方便脚本、面板、机器人远程调用 |
| **ICU RCON Client** | 桌面客户端：原生 Go GUI，通过上述 API（或直连 RCON）管理游戏服务器，单文件运行 |

两个程序均为 Go 编写的**单文件可执行程序**，无运行时依赖。

> 📖 **新手请先看 [docs/使用教程.md](docs/使用教程.md)** —— 从"这个项目是干什么的"讲到部署守护进程、
> 在 ICU 服务器导航里认领服务器、用桌面客户端连接，每一步都有截图和终端输出示例。

---

## 目录

- [ICURconDaemon（守护进程）](#icurcondaemon守护进程)
  - [特性](#特性)
  - [快速开始](#快速开始)
  - [config.ini](#configini)
  - [HTTP API](#http-api)
  - [从源码构建](#从源码构建)
- [ICU RCON Client（桌面客户端）](#icu-rcon-client桌面客户端)
  - [功能一览](#功能一览)
  - [安装与启动](#安装与启动)
  - [使用教程](#使用教程)
  - [servers.json](#serversjson)
  - [常见问题与错误处理](#常见问题与错误处理)
- [完整图文教程](docs/使用教程.md)
- [v1.1.0 优化说明](#v110-优化说明)
- [开发与测试](#开发与测试)
- [许可证](#许可证)

---

## ICURconDaemon（守护进程）

### 特性

- 将 RCON 协议转为 HTTP REST API（单接口，零配置即可对接）
- 首次运行交互式配置（端口和认证密钥），保存到 `config.ini` 后续自动加载
- 自动生成 UUID v4 作为认证密钥（也可自定义）
- 多语言支持：自动检测系统语言（中文/英文），**Windows 下也能正确识别中文系统**
- 跨平台：Windows & Linux，`CGO_ENABLED=0` 即可交叉编译
- **零第三方依赖**（v1.1.0 起仅使用 Go 标准库）

### 快速开始

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

- **端口**：默认 `8080`，按回车使用默认值，或输入 1-65535 之间的自定义端口（会校验合法性）。
- **AuthKey**：默认生成随机 UUID，按回车使用默认值，或输入自定义密钥。

4. 配置保存到 `config.ini`，后续启动直接读取。按 `Ctrl+C` 可优雅退出。

### config.ini

```ini
# ICURconDaemon Config
port=8080
authkey=你的认证密钥
```

> 注意：若 `config.ini` 中缺少端口或 AuthKey（例如 AuthKey 为空），守护进程会视为未配置并重新进入首次运行向导——空密钥会放行所有请求，属于安全隐患。

### HTTP API

接口自 v1.0.0 起**保持不变**：

#### `POST /api/icu/daemon/rcon`

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

- `status`：`1` = 成功，`0` = 失败（失败原因见 `msg`）
- `auth`：必须与 `config.ini` 中的 `authkey` 一致
- 非 POST 请求返回 `405`；请求体非法返回 `400`；守护进程不可用时客户端会收到连接错误
- 服务端已启用读写超时与请求体大小限制（64KB），可放心暴露在可信内网

示例：

```bash
curl -X POST http://127.0.0.1:8080/api/icu/daemon/rcon \
  -H "Content-Type: application/json" \
  -d '{"ip":"127.0.0.1","port":25575,"passwd":"rcon密码","msg":"list","auth":"你的认证密钥"}'
```

### 从源码构建

需要 Go 1.21+，无任何第三方依赖：

```bash
go build -trimpath -ldflags "-s -w -X main.Version=v1.1.0" -o ICURconDaemon .
```

---

## ICU RCON Client（桌面客户端）

一个**原生 Go GUI 客户端**（基于 [Gio UI](https://gioui.org)，无 Electron、无浏览器内核、无 CGO），内嵌中文字体，单文件即开即用。

### 功能一览

- **服务器信息列表**：卡片式列表，支持添加 / 编辑 / 删除 / 查看详情
- **两种连接方式**：
  - **Daemon 转发**（默认）：走本项目的 `POST /api/icu/daemon/rcon` 接口
  - **直连 RCON**：不经过守护进程，客户端直接与游戏服务器建立 RCON 会话
- **便捷启动**：所有服务器信息保存在程序同目录的 `servers.json`，下次启动自动恢复上次选择的服务器，双击卡片即可快速测试连接
- **RCON 面板**：
  - 命令输入框（回车发送，`↑` / `▼` 按钮或方向键切换历史）
  - 命令历史（每服务器独立保存最近 200 条，点击历史条目回填命令）
  - 实时输出（带时间戳、耗时毫秒数；命令/返回/错误/系统四种颜色区分）
  - 点击任意输出行复制该行；支持“复制全部”和“清屏”
  - **强制停止**：命令执行期间「发送」按钮变为红色「停止」，点击可中断卡死的请求
  - **连接测试**：发送 `help` 命令验证链路（几乎所有 RCON 实现都支持且无副作用）
- **明暗主题**：自动跟随系统深浅色模式，也可点击左下角按钮手动切换（选择会保存）
- **错误处理**：连接失败、认证失败、超时、非法端口、JSON 损坏等情况均有明确的界面提示，`servers.json` 损坏时自动备份并重建
- **中英双语**：跟随系统语言，可一键切换
- **跨平台**：Windows / Linux 提供 Release 版（Linux 构建基于 Ubuntu 22.04，要求 glibc ≥ 2.35）；macOS 在目标系统上构建后可用

### 安装与启动

从 [Releases](../../releases) 下载对应平台的客户端：

| 文件 | 平台 |
|---|---|
| `ICURconClient-windows-amd64.exe` | Windows 10/11 x64 |
| `ICURconClient-linux-amd64` | Linux x64（glibc ≥ 2.35：Ubuntu 22.04+ / Debian 12+，需桌面图形环境） |
| macOS | 请在目标系统上自行构建（Gio 的 GL 后端需要 CGO），见[开发与测试](#开发与测试) |

Windows 版使用 `-H=windowsgui` 构建，启动后**不弹出黑色控制台窗口**。

直接双击运行即可，无需安装。首次启动会在程序同目录生成 `servers.json`（保存服务器列表）。

### 使用教程

#### 1. 添加服务器

点击左侧栏的 **「＋ 添加服务器」**，在弹出的表单中填写：

1. **名称**：随意起一个方便识别的名字
2. **连接方式**：
   - **Daemon 转发**：填写运行 ICURconDaemon 的主机地址和端口（默认 8080），以及 `config.ini` 里的 **AuthKey**
   - **直连 RCON**：跳过 Daemon 部分，直接填游戏服务器信息
3. **RCON 信息**：游戏服务器地址、RCON 端口（Minecraft 默认 25575，需在 `server.properties` 中开启 `enable-rcon=true`）、RCON 密码（`rcon.password`）
4. 点击 **「保存」**

#### 2. 测试连接

选中服务器后，点击右上角 **「测试连接」**：

- 成功：顶部状态变为绿色「已连接 · xx ms」，输出区显示耗时
- 失败：输出区以红色显示具体原因（见[常见问题](#常见问题与错误处理)）

#### 3. 执行 RCON 命令

在底部输入框输入命令（如 `list`、`say hello`、`whitelist add Steve`），按 **回车** 或点击 **「发送」**：

- 执行中的命令未返回前，同一服务器不会重复发送（防止 RCON 串包）
- 输出区实时显示返回内容与耗时
- 每条命令自动记入该服务器的历史，`↑`/`↓` 可快速调出

#### 4. 管理 / 切换服务器

- 左侧卡片**单击**选中，**双击**选中并测试连接
- 选中后可用列表下方的 **详情 / 编辑 / 删除** 按钮（删除有二次确认）
- 所有修改即时保存到 `servers.json`

#### 5. 语言切换

左下角点击 **English / 中文** 即时切换界面语言。

### servers.json

与客户端可执行文件同目录，形如：

```json
{
  "version": 1,
  "lastSelected": "a3f2...-...",
  "servers": [
    {
      "id": "a3f2...-...",
      "name": "我的生存服",
      "mode": "daemon",
      "daemonHost": "127.0.0.1",
      "daemonPort": 8080,
      "authKey": "xxxxxxxx-xxxx-4xxx-...",
      "rconHost": "10.0.0.5",
      "rconPort": 25575,
      "rconPass": "rcon密码",
      "note": "主服",
      "history": ["list", "say hello"],
      "createdAt": "2025-01-01T12:00:00Z",
      "lastUsedAt": "2025-01-02T08:30:00Z"
    }
  ]
}
```

- 保存采用「写临时文件 + 原子重命名」，进程崩溃也不会损坏文件
- 文件包含 RCON 密码与 AuthKey，权限为 `0600`，请**不要**把 `servers.json` 提交到仓库或分享给他人
- 若文件损坏，客户端会将其重命名为 `servers.json.corrupt-时间戳` 并以空列表继续运行，同时在界面上提示

### 常见问题与错误处理

| 现象（输出区提示） | 原因与解决 |
|---|---|
| `连接RCON服务器失败: ...connection refused`（Daemon 转发模式） | 游戏服务器 RCON 未开启或地址/端口错误。检查 `server.properties` 的 `enable-rcon=true`、`rcon.port`；确认 RCON 地址不是写的 Daemon 地址 |
| `无效的认证密钥` | 客户端填写的 AuthKey 与守护进程 `config.ini` 的 `authkey` 不一致 |
| `RCON 认证失败`（直连模式） | RCON 密码错误 |
| `dial tcp ... i/o timeout` | 网络不通 / 防火墙拦截 / Daemon 或游戏服务器未启动 |
| `HTTP 400: invalid character ...` | 请求体异常（极少见）；多为中间代理损坏了请求 |
| `认证时返回了不可用的数据` | 对端不是标准 Source RCON 服务（某些 mod 的 RCON 实现不标准），或端口填成了非 RCON 服务 |
| 客户端启动后列表为空 | `servers.json` 被删除或损坏，查看同目录的 `servers.json.corrupt-*` 备份 |
| Daemon 模式下命令一直无返回 | RCON 命令耗时过长（如整存整取）；守护进程最长等待 2 分钟，客户端最长等待 140 秒 |

---

## v1.1.0 优化说明

在**完全保持 HTTP API 与 `config.ini` 格式不变**的前提下：

- **零第三方依赖**：移除 `gorilla/mux` 与 `google/uuid`（改用标准库路由与 `crypto/rand` 实现 UUID v4），构建更快、二进制更小、供应链攻击面更小
- **修复 RCON 连接泄漏**：原实现在认证失败时不会关闭已建立的 TCP 连接，长期运行会累积泄漏；现已确保任何握手失败路径都关闭连接（含回归测试）
- **重写 RCON 包读写**：
  - 使用 `io.ReadFull` 精确读取定长包头/包体，不再依赖 `bytes.Buffer` + `binary.Read`（反射），每条命令的编解码从多次分配降为一次
  - 修复超大包（`dataSize` 接近缓冲区上限）时的越界 panic 隐患
- **HTTP 服务加固**：`http.Server` 全套超时（读头 5s / 读 15s / 写 150s / 空闲 60s）、64KB 请求体上限、方法白名单、优雅退出（Ctrl+C 后等待在途请求）
- **配置健壮性**：端口合法性校验、空 AuthKey 视为未配置、CRLF 兼容
- **Windows 中文识别**：通过 `GetUserDefaultUILanguage` 检测系统 UI 语言，中文 Windows 不再显示英文提示
- **可观测性**：每次命令记录目标、耗时；认证失败记录来源 IP

---

## 开发与测试

```bash
# 守护进程：构建 + 全量测试（协议单元测试 + HTTP 端到端测试）
go build ./... && go test ./... -count=1

# 客户端：构建（无 GUI 逻辑自测）
cd client
go test ./... -count=1        # 需要图形环境时可跳过，store 纯逻辑可在任意环境运行
go build -trimpath -ldflags "-s -w -H=windowsgui" -o ../ICURconClient.exe .
./ICURconClient --selftest    # 无窗口自检（store 持久化 / UUID）
```

客户端构建依赖：Go 1.24+。**Windows** 版可直接 `CGO_ENABLED=0` 构建为单文件；**Linux / macOS** 版因 Gio 的 GL 后端使用 CGO，需在目标系统上构建：

```bash
# Debian/Ubuntu 依赖
sudo apt install libgl1-mesa-dev xorg-dev
cd client
go build -trimpath -ldflags "-s -w -X main.clientVersion=v1.1.0" -o ../ICURconClient .
```

---

## 许可证

- 本项目代码：[MIT](LICENSE)
- 客户端内嵌字体 [Noto Sans SC](https://fonts.google.com/noto/specimen/Noto+Sans+SC)：[SIL Open Font License 1.1](client/assets/fonts/OFL.txt)
