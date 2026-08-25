//go:build !linux && !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func platformDefaultDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".elec"
	}
	return filepath.Join(dir, "WxxyshallMonitoring")
}
func platformInitialize()               {}
func platformReportError(string, error) {}
func platformHelpText() string          { return linuxHelpText() }

func platformListenAddr(port int) string                  { return fmt.Sprintf("127.0.0.1:%d", port) }
func platformPrepareRun() (bool, error)                   { return false, nil }
func platformConfigureLogging(string) error               { return nil }
func secureDataPermissions(data string) error             { return secureDataPermissionsUnix(data) }
func platformValidateDataPath(data string) error          { return rejectSymlinkComponents(data) }
func platformShouldOpenDashboard() bool                   { return false }
func platformOpenDashboard(int, string) error             { return nil }
func platformStartUI(int, string, chan<- os.Signal) error { return nil }
func cmdInstallPlatform()                                 { fmt.Println("当前平台不支持自动安装") }
func cmdStatusPlatform()                                  { fmt.Println("当前平台不支持服务状态命令") }
func cmdLogsPlatform()                                    { fmt.Println("当前平台不支持服务日志命令") }
