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
		val := os.Getenv(env)
		if strings.Contains(strings.ToLower(val), "zh") {
			return langZH
		}
	}
	return langEN
}

type messages struct {
	FirstRunTitle          string
	FirstRunHint           string
	PortPrompt             string
	AuthKeyPrompt          string
	ConfigSaved            string
	YourAuthKey            string
	KeepAuthKeySafe        string
	FailedSaveConfig       string
	Starting               string
	InvalidAuthKey         string
	FailedConnectRCON      string
	FailedExecRCON         string
	FailedReadRCON         string
	RCONIDMismatch         string
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
		Starting:          "ICURconDaemon v%s 启动于 :%s",
		InvalidAuthKey:    "无效的认证密钥",
		FailedConnectRCON: "连接RCON服务器失败: %v",
		FailedExecRCON:    "执行RCON命令失败: %v",
		FailedReadRCON:    "读取RCON消息失败: %v",
		RCONIDMismatch:    "RCON响应ID不匹配",
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
		Starting:          "ICURconDaemon v%s starting on :%s",
		InvalidAuthKey:    "Invalid authentication key",
		FailedConnectRCON: "Failed to connect to RCON server: %v",
		FailedExecRCON:    "Failed to execute RCON command: %v",
		FailedReadRCON:    "Failed to read RCON message: %v",
		RCONIDMismatch:    "RCON ID mismatch",
	},
}

func t() messages {
	return msgMap[currentLang]
}
