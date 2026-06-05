package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
)

type Config struct {
	Port    string
	AuthKey string
}

const configFile = "config.ini"

func loadConfig() *Config {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil
	}
	cfg := &Config{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "port":
			cfg.Port = value
		case "authkey":
			cfg.AuthKey = value
		}
	}
	return cfg
}

func saveConfig(cfg *Config) error {
	content := fmt.Sprintf("# ICURconDaemon Config\nport=%s\nauthkey=%s\n", cfg.Port, cfg.AuthKey)
	return os.WriteFile(configFile, []byte(content), 0644)
}

func promptConfig() *Config {
	reader := bufio.NewReader(os.Stdin)
	m := t()

	fmt.Println(m.FirstRunTitle)
	fmt.Println(m.FirstRunHint)

	fmt.Print(m.PortPrompt)
	portInput, _ := reader.ReadString('\n')
	portInput = strings.TrimSpace(portInput)
	port := "8080"
	if portInput != "" {
		port = portInput
	}

	fmt.Print(m.AuthKeyPrompt)
	authInput, _ := reader.ReadString('\n')
	authInput = strings.TrimSpace(authInput)
	authKey := uuid.New().String()
	if authInput != "" {
		authKey = authInput
	}

	cfg := &Config{Port: port, AuthKey: authKey}
	if err := saveConfig(cfg); err != nil {
		log.Fatalf(m.FailedSaveConfig, err)
	}
	fmt.Printf(m.ConfigSaved+"\n", configFile)
	fmt.Printf(m.YourAuthKey+"\n", authKey)
	fmt.Println(m.KeepAuthKeySafe)
	return cfg
}
