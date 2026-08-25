//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultDataDir() string {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		root, _ = os.UserCacheDir()
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "WxxyshallMonitoring", "data")
}
