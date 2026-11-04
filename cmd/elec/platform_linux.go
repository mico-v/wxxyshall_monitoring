//go:build linux

package main

import (
	"net"
	"os"
	"strconv"
)

func platformDefaultDir() string { return installDir }

func platformInitialize() {}

func platformReportError(string, error) {}

func platformHelpText() string { return linuxHelpText() }

// platformListenAddr 返回监听地址。
// bind 为空时沿用历史行为：监听所有地址（":port"）；否则只绑指定地址。
func platformListenAddr(port int, bind string) string {
	if bind != "" {
		return net.JoinHostPort(bind, strconv.Itoa(port))
	}
	return ":" + strconv.Itoa(port)
}

func platformPrepareRun() (bool, error) { return false, nil }

func platformConfigureLogging(string) error { return nil }

func secureDataPermissions(data string) error { return secureDataPermissionsUnix(data) }

func platformValidateDataPath(data string) error { return rejectSymlinkComponents(data) }

func platformShouldOpenDashboard() bool { return false }

func platformOpenDashboard(int, string) error { return nil }

func platformStartUI(int, string, chan<- os.Signal) error { return nil }

func cmdInstallPlatform() { cmdInstallLinux() }

func cmdStatusPlatform() { cmdStatusLinux() }

func cmdLogsPlatform() { cmdLogsLinux() }
