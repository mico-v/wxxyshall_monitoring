//go:build !linux && !windows

package config

import (
	"os"
	"path/filepath"
)

func defaultDataDir() string {
	root, err := os.UserConfigDir()
	if err != nil {
		root = "."
	}
	return filepath.Join(root, "WxxyshallMonitoring", "data")
}
