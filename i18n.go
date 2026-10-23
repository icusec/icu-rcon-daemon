package main

import (
	"os"
	"strings"
)

type lang string

const (
	langZH lang = "zh"
	langEN lang = "en"
)

var currentLang lang

func initLang() {
	currentLang = detectLang()
}

func detectLang() lang {
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

type messages struct {
	FirstRunTitle     string
	FirstRunHint      string
	PortPrompt        string
	AuthKeyPrompt     string
	ConfigSaved       string
	YourAuthKey       string
	KeepAuthKeySafe   string
	FailedSaveConfig  string
	Starting          string
	InvalidAuthKey    string
	InvalidTarget     string
	MsgRequired       string
	CommandTooLong    string
	BatchEmpty        string
	BatchTooMany      string
	BatchTimeout      string
	FailedConnectRCON string
	FailedExecRCON    string
	FailedReadRCON    string
	MethodNotAllowed  string
	ListenFailed      string
	ShuttingDown      string
	InvalidPortInput  string
}

var msgMap = map[lang]messages{
	langZH: {
		FirstRunTitle:     "=== ICURconDaemon 首次运行配置 ===",
		FirstRunHint:      "按回车键使用默认值。",
		PortPrompt:        "端口 [8080]: ",
		AuthKeyPrompt:     "AuthKey [随机UUID]: ",
		ConfigSaved:       "配置已保存到 %s",
		YourAuthKey:       "你的 AuthKey: %s",
		KeepAuthKeySafe:   "请妥善保管你的 AuthKey！",
		FailedSaveConfig:  "保存配置失败: %v",
		Starting:          "ICURconDaemon %s 启动于 :%s",
		InvalidAuthKey:    "无效的认证密钥",
		InvalidTarget:     "ip/port/passwd 参数缺失或无效",
		MsgRequired:       "缺少 msg 参数",
		CommandTooLong:    "命令太长（上限 %d 字节）",
		BatchEmpty:        "msgs 不能为空",
		BatchTooMany:      "msgs 最多 %d 条",
		BatchTimeout:      "批量请求超时",
		FailedConnectRCON: "连接RCON服务器失败: %v",
		FailedExecRCON:    "执行RCON命令失败: %v",
		FailedReadRCON:    "读取RCON消息失败: %v",
		MethodNotAllowed:  "仅支持 POST 请求",
		ListenFailed:      "服务启动失败: %v",
		ShuttingDown:      "正在退出，等待连接收尾…",
		InvalidPortInput:  "无效的端口号，请输入 1-65535 之间的数字。",
	},
	langEN: {
		FirstRunTitle:     "=== ICURconDaemon First Run Configuration ===",
		FirstRunHint:      "Press Enter to use default value.",
		PortPrompt:        "Port [8080]: ",
		AuthKeyPrompt:     "AuthKey [random UUID]: ",
		ConfigSaved:       "Config saved to %s",
		YourAuthKey:       "Your AuthKey: %s",
		KeepAuthKeySafe:   "Please keep your AuthKey safe!",
		FailedSaveConfig:  "Failed to save config: %v",
		Starting:          "ICURconDaemon %s starting on :%s",
		InvalidAuthKey:    "Invalid authentication key",
		InvalidTarget:     "ip/port/passwd are missing or invalid",
		MsgRequired:       "msg is required",
		CommandTooLong:    "Command too long (max %d bytes)",
		BatchEmpty:        "msgs must be a non-empty array",
		BatchTooMany:      "at most %d commands",
		BatchTimeout:      "batch request timed out",
		FailedConnectRCON: "Failed to connect to RCON server: %v",
		FailedExecRCON:    "Failed to execute RCON command: %v",
		FailedReadRCON:    "Failed to read RCON message: %v",
		MethodNotAllowed:  "Only POST is supported",
		ListenFailed:      "Failed to start server: %v",
		ShuttingDown:      "Shutting down, draining connections…",
		InvalidPortInput:  "Invalid port, please enter a number between 1 and 65535.",
	},
}

func t() messages {
	if m, ok := msgMap[currentLang]; ok {
		return m
	}
	// Uninitialised language (for example in tests) falls back to English
	// rather than the zero value, which would emit empty messages.
	return msgMap[langEN]
}
