package config

import (
	"embed"
	"os"
)

//go:embed config.example.json
var exampleConfig embed.FS

// GenerateDefaultConfig 如果 config.json 不存在，从嵌入的示例创建。
func GenerateDefaultConfig() error {
	path := ConfigPath()
	if _, err := os.Stat(path); err == nil {
		return nil // 已存在
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := exampleConfig.ReadFile("config.example.json")
	if err != nil {
		return err
	}

	return writeFileAtomic(path, data, 0640)
}
