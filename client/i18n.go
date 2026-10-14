package main

import (
	"os"
	"strings"
)

type langKey string

const (
	langZH langKey = "zh"
	langEN langKey = "en"
)

// ui holds every user-facing string. The zero value is the English variant;
// zh mirrors it with Chinese text.
type uiStrings struct {
	AppName      string
	AppSub       string
	AddServer    string
	Servers      string
	EmptyList    string
	Edit         string
	Delete       string
	Details      string
	Connect      string
	Disconnect   string
	TestConn     string
	ModeDaemon   string
	ModeDirect   string
	NotSelected  string
	Idle         string
	Connecting   string
	ConnOK       string
	ConnFail     string
	InputHint    string
	Send         string
	Stop         string
	Cancelled    string
	Stopped      string
	LightTheme   string
	DarkTheme    string
	History      string
	ClearHist    string
	ClearOutput  string
	Output       string
	NoHistory    string
	CopyAll      string
	Copied       string
	Ready        string
	AddTitle     string
	EditTitle    string
	DetailsTitle string
	DelTitle     string
	DelBody      string
	Cancel       string
	Save         string
	Close        string
	FieldName    string
	FieldMode    string
	FieldDHost   string
	FieldDPort   string
	FieldAuthKey string
	FieldRHost   string
	FieldRPort   string
	FieldRPass   string
	FieldNote    string
	ShowPass     string
	HidePass     string
	ErrNameReq   string
	ErrHostReq   string
	ErrPortInv   string
	ErrAuthReq   string
	ErrSave      string
	ErrLoad      string
	Disconnected string
	TestOK       string
	TestFail     string
	SendFail     string
	LatencyFmt   string
	LangToggle   string
	DirExe       string
	LineCmd      string
	LineResp     string
	LineErr      string
	LineSys      string
	Version      string
	NoServers    string
	OptConfirm   string
}

var zh = uiStrings{
	AppName:      "ICU RCON 客户端",
	AppSub:       "通过 ICURconDaemon 或直连管理游戏服务器",
	AddServer:    "＋ 添加服务器",
	Servers:      "服务器",
	EmptyList:    "还没有服务器，\n点击上方按钮添加。",
	Edit:         "编辑",
	Delete:       "删除",
	Details:      "详情",
	Connect:      "连接",
	Disconnect:   "断开",
	TestConn:     "测试连接",
	ModeDaemon:   "Daemon 转发",
	ModeDirect:   "直连 RCON",
	NotSelected:  "在左侧选择一个服务器，或添加新服务器开始使用",
	Idle:         "未连接",
	Connecting:   "连接中…",
	ConnOK:       "已连接",
	ConnFail:     "连接失败",
	InputHint:    "输入 RCON 命令，回车发送，↑↓ 切换历史",
	Send:         "发送",
	Stop:         "停止",
	Cancelled:    "已强制停止上一条命令",
	Stopped:      "已停止",
	LightTheme:  "浅色主题",
	DarkTheme:   "深色主题",
	History:      "历史命令",
	ClearHist:    "清空",
	ClearOutput:  "清屏",
	Output:       "输出",
	NoHistory:    "暂无历史命令",
	CopyAll:      "复制全部",
	Copied:       "已复制到剪贴板",
	Ready:        "就绪",
	AddTitle:     "添加服务器",
	EditTitle:    "编辑服务器",
	DetailsTitle: "服务器详情",
	DelTitle:     "删除服务器",
	DelBody:      "确定要删除「%s」吗？该操作不可恢复。",
	Cancel:       "取消",
	Save:         "保存",
	Close:        "关闭",
	FieldName:    "名称",
	FieldMode:    "连接方式",
	FieldDHost:   "Daemon 地址（运行 ICURconDaemon 的主机）",
	FieldDPort:   "Daemon 端口",
	FieldAuthKey: "Daemon AuthKey（config.ini 中的 authkey）",
	FieldRHost:   "RCON 地址（游戏服务器主机）",
	FieldRPort:   "RCON 端口",
	FieldRPass:   "RCON 密码",
	FieldNote:    "备注（可选）",
	ShowPass:     "显示",
	HidePass:     "隐藏",
	ErrNameReq:   "请填写名称",
	ErrHostReq:   "请填写地址",
	ErrPortInv:   "端口必须是 1-65535 的数字",
	ErrAuthReq:   "请填写 AuthKey",
	ErrSave:      "保存失败: %v",
	ErrLoad:      "servers.json 已损坏，原文件已备份为 %s",
	Disconnected: "已断开",
	TestOK:       "连接测试成功",
	TestFail:     "连接测试失败",
	SendFail:     "发送失败",
	LatencyFmt:   "%dms",
	LangToggle:   "English",
	DirExe:       "配置文件目录",
	LineCmd:      "命令",
	LineResp:     "返回",
	LineErr:      "错误",
	LineSys:      "系统",
	Version:      "版本",
	NoServers:    "尚未添加服务器",
	OptConfirm:   "确定",
}

var en = uiStrings{
	AppName:      "ICU RCON Client",
	AppSub:       "Manage game servers via ICURconDaemon or direct RCON",
	AddServer:    "＋ Add Server",
	Servers:      "Servers",
	EmptyList:    "No servers yet.\nClick the button above to add one.",
	Edit:         "Edit",
	Delete:       "Delete",
	Details:      "Details",
	Connect:      "Connect",
	Disconnect:   "Disconnect",
	TestConn:     "Test Connection",
	ModeDaemon:   "Daemon",
	ModeDirect:   "Direct RCON",
	NotSelected:  "Select a server on the left, or add a new one to get started",
	Idle:         "Idle",
	Connecting:   "Connecting…",
	ConnOK:       "Connected",
	ConnFail:     "Failed",
	InputHint:    "Type an RCON command, Enter to send, ↑↓ for history",
	Send:         "Send",
	Stop:        "Stop",
	Cancelled:   "Previous command cancelled",
	Stopped:     "Stopped",
	LightTheme:  "Light theme",
	DarkTheme:   "Dark theme",
	History:      "History",
	ClearHist:    "Clear",
	ClearOutput:  "Clear",
	Output:       "Output",
	NoHistory:    "No history yet",
	CopyAll:      "Copy All",
	Copied:       "Copied to clipboard",
	Ready:        "Ready",
	AddTitle:     "Add Server",
	EditTitle:    "Edit Server",
	DetailsTitle: "Server Details",
	DelTitle:     "Delete Server",
	DelBody:      "Delete \"%s\"? This cannot be undone.",
	Cancel:       "Cancel",
	Save:         "Save",
	Close:        "Close",
	FieldName:    "Name",
	FieldMode:    "Connection Mode",
	FieldDHost:   "Daemon host (machine running ICURconDaemon)",
	FieldDPort:   "Daemon port",
	FieldAuthKey: "Daemon AuthKey (authkey in config.ini)",
	FieldRHost:   "RCON host (game server host)",
	FieldRPort:   "RCON port",
	FieldRPass:   "RCON password",
	FieldNote:    "Note (optional)",
	ShowPass:     "Show",
	HidePass:     "Hide",
	ErrNameReq:   "Name is required",
	ErrHostReq:   "Host is required",
	ErrPortInv:   "Port must be a number between 1 and 65535",
	ErrAuthReq:   "AuthKey is required",
	ErrSave:      "Failed to save: %v",
	ErrLoad:      "servers.json is corrupted, backed up as %s",
	Disconnected: "Disconnected",
	TestOK:       "Connection test succeeded",
	TestFail:     "Connection test failed",
	SendFail:     "Send failed",
	LatencyFmt:   "%dms",
	LangToggle:   "中文",
	DirExe:       "Config directory",
	LineCmd:      "CMD",
	LineResp:     "RESP",
	LineErr:      "ERR",
	LineSys:      "SYS",
	Version:      "Version",
	NoServers:    "No servers added yet",
	OptConfirm:   "OK",
}

func stringsFor(l langKey) uiStrings {
	if l == langZH {
		return zh
	}
	return en
}

var langOverride langKey

func currentLang() langKey {
	if langOverride != "" {
		return langOverride
	}
	return detectLang()
}

func detectLang() langKey {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if strings.Contains(strings.ToLower(os.Getenv(env)), "zh") {
			return langZH
		}
	}
	if sl := systemLang(); sl != "" {
		return sl
	}
	return langEN
}
