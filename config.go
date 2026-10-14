package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port    string
	AuthKey string
}

const configFile = "config.ini"

// validPort reports whether s is a usable TCP port.
func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

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
	// A config missing a usable port or auth key is treated as not configured
	// (an empty auth key would otherwise accept every request).
	if !validPort(cfg.Port) || cfg.AuthKey == "" {
		return nil
	}
	return cfg
}

func saveConfig(cfg *Config) error {
	content := fmt.Sprintf("# ICURconDaemon Config\nport=%s\nauthkey=%s\n", cfg.Port, cfg.AuthKey)
	return os.WriteFile(configFile, []byte(content), 0o600)
}

func promptConfig() *Config {
	reader := bufio.NewReader(os.Stdin)
	m := t()

	fmt.Println(m.FirstRunTitle)
	fmt.Println(m.FirstRunHint)

	var port string
	for {
		fmt.Print(m.PortPrompt)
		portInput, _ := reader.ReadString('\n')
		portInput = strings.TrimSpace(portInput)
		if portInput == "" {
			port = "8080"
			break
		}
		if validPort(portInput) {
			port = portInput
			break
		}
		fmt.Println(m.InvalidPortInput)
	}

	fmt.Print(m.AuthKeyPrompt)
	authInput, _ := reader.ReadString('\n')
	authInput = strings.TrimSpace(authInput)
	authKey := newUUID()
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

// newUUID returns a random RFC 4122 version 4 UUID without external deps.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Practically unreachable; keep running even if the system RNG fails.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
