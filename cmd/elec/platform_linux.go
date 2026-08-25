//go:build linux

package main

import (
	"fmt"
	"os"
)

func platformDefaultDir() string { return installDir }

func platformInitialize() {}

func platformReportError(string, error) {}

func platformHelpText() string { return linuxHelpText() }

func platformListenAddr(port int) string { return fmt.Sprintf(":%d", port) }

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
