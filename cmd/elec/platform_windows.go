//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/mico-v/wxxyshall-monitoring/internal/config"
)

const windowsInstallName = "WxxyshallMonitoring"

var windowsMutex windows.Handle

func platformInitialize() {
	if os.Getenv("ELEc_DIR") != "" {
		return
	}
	for i, arg := range os.Args[1:] {
		if arg == "--root" && i+2 <= len(os.Args[1:]) {
			_ = os.Setenv("ELEc_DIR", os.Args[i+2])
			return
		}
		if strings.HasPrefix(arg, "--root=") {
			_ = os.Setenv("ELEc_DIR", strings.TrimPrefix(arg, "--root="))
			return
		}
	}
	if executable, err := os.Executable(); err == nil {
		root := filepath.Dir(executable)
		if _, err := os.Stat(filepath.Join(root, "data", "config.json")); err == nil {
			_ = os.Setenv("ELEc_DIR", root)
		}
	}
}

func platformHelpText() string {
	return `宿舍电费监控 — Windows 管理工具

用法:
  elec.exe                 首次运行自动安装，之后打开仪表盘
  elec.exe run             启动后台服务和系统托盘
  elec.exe install         安装到 %LOCALAPPDATA% 并设置开机启动
  elec.exe collect         单次采集
  elec.exe status          查看安装和自启动状态
  elec.exe token           查看 token 状态
  elec.exe config          查看配置路径

托盘:
  双击图标打开仪表盘；右键可打开配置目录、切换开机启动或退出。

环境变量:
  ELEc_DIR    覆盖安装/数据根目录
  ADMIN_KEY   覆盖自动生成的管理密钥
`
}

func platformDefaultDir() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, windowsInstallName)
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, windowsInstallName)
	}
	return filepath.Join(".", windowsInstallName)
}

func platformListenAddr(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }

func platformConfigureLogging(data string) error {
	file, err := os.OpenFile(filepath.Join(data, "elec.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})))
	return nil
}

// Windows 的 chmod 语义与 Unix 不同，文件 ACL 由当前用户目录权限负责。
func secureDataPermissions(data string) error {
	return filepath.WalkDir(data, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("数据目录不允许符号链接: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("数据目录只允许普通文件: %s", path)
		}
		return nil
	})
}

func platformValidateDataPath(data string) error {
	absPath, err := filepath.Abs(data)
	if err != nil {
		return fmt.Errorf("解析绝对路径失败: %w", err)
	}
	volume := filepath.VolumeName(absPath)
	if volume == "" {
		return fmt.Errorf("路径缺少 Windows 卷名: %s", absPath)
	}
	rest := strings.TrimLeft(strings.TrimPrefix(absPath, volume), `\/`)
	current := volume + string(os.PathSeparator)
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("检查路径组件 %s 失败: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("路径组件不能是符号链接: %s", current)
		}
	}
	return nil
}

func platformPrepareRun() (bool, error) {
	target := filepath.Join(elecDir(), "elec.exe")
	current, err := os.Executable()
	if err != nil {
		return false, err
	}
	current, _ = filepath.Abs(current)
	target, _ = filepath.Abs(target)
	if !strings.EqualFold(current, target) {
		alreadyRunning, err := acquireWindowsMutex()
		if err != nil {
			return false, err
		}
		if alreadyRunning {
			_ = openRunningWindowsDashboard()
			return true, nil
		}
		if err := installWindowsBinary(current, target); err != nil {
			return false, err
		}
		if err := registerWindowsStartup(target); err != nil {
			return false, err
		}
		if windowsMutex != 0 {
			_ = windows.CloseHandle(windowsMutex)
			windowsMutex = 0
		}
		cmd := exec.Command(target, "run", "--background", "--open", "--root", elecDir())
		cmd.Dir = elecDir()
		if err := cmd.Start(); err != nil {
			return false, fmt.Errorf("启动已安装程序失败: %w", err)
		}
		return true, nil
	}

	alreadyRunning, err := acquireWindowsMutex()
	if err != nil {
		return false, err
	}
	if alreadyRunning {
		_ = openRunningWindowsDashboard()
		return true, nil
	}
	return false, nil
}

func acquireWindowsMutex() (bool, error) {
	name, err := windows.UTF16PtrFromString(`Local\WxxyshallMonitoring`)
	if err != nil {
		return false, err
	}
	windowsMutex, err = windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(windowsMutex)
		windowsMutex = 0
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func openRunningWindowsDashboard() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	keyData, err := os.ReadFile(filepath.Join(dataDir(), ".admin_key"))
	if err != nil {
		return err
	}
	return platformOpenDashboard(cfg.Port, strings.TrimSpace(string(keyData)))
}

func installWindowsBinary(current, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if strings.EqualFold(current, target) {
		return nil
	}
	backup := target + ".old"
	backedUp := false
	if _, err := os.Stat(target); err == nil {
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("准备替换旧程序失败: %w", err)
		}
		backedUp = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := copyFile(current, target); err != nil {
		if backedUp {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("复制程序失败: %w", err)
	}
	if backedUp {
		_ = os.Remove(backup)
	}
	return nil
}

func registerWindowsStartup(target string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	command := fmt.Sprintf(`"%s" run --background --root "%s"`, target, elecDir())
	return key.SetStringValue(windowsInstallName, command)
}

func unregisterWindowsStartup() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	err = key.DeleteValue(windowsInstallName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func windowsAutostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(windowsInstallName)
	return err == nil
}

func launchWindowsBackground(target string, open bool) error {
	args := []string{"run", "--background"}
	if open {
		args = append(args, "--open")
	}
	args = append(args, "--root", elecDir())
	cmd := exec.Command(target, args...)
	cmd.Dir = elecDir()
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

func cmdInstallPlatform() {
	current, err := os.Executable()
	if err != nil {
		fmt.Printf("安装失败: %v\n", err)
		return
	}
	target := filepath.Join(elecDir(), "elec.exe")
	alreadyRunning, err := acquireWindowsMutex()
	if err != nil {
		fmt.Printf("安装失败: %v\n", err)
		return
	}
	if alreadyRunning {
		_ = openRunningWindowsDashboard()
		return
	}
	if err := installWindowsBinary(current, target); err != nil {
		fmt.Printf("安装失败: %v\n", err)
		return
	}
	if err := registerWindowsStartup(target); err != nil {
		fmt.Printf("设置开机启动失败: %v\n", err)
		return
	}
	if windowsMutex != 0 {
		_ = windows.CloseHandle(windowsMutex)
		windowsMutex = 0
	}
	if err := launchWindowsBackground(target, true); err != nil {
		fmt.Printf("启动后台服务失败: %v\n", err)
		return
	}
	fmt.Printf("已安装到 %s，并设置当前用户开机启动\n", elecDir())
}

func cmdStatusPlatform() {
	if windowsAutostartEnabled() {
		fmt.Printf("Windows 自启动: 已配置\n数据目录: %s\n", dataDir())
		return
	}
	fmt.Printf("Windows 自启动: 未配置\n数据目录: %s\n", dataDir())
}

func cmdLogsPlatform() {
	fmt.Printf("Windows 后台日志输出到当前进程标准日志；数据目录: %s\n", dataDir())
}

func platformShouldOpenDashboard() bool {
	background := false
	for _, arg := range os.Args[1:] {
		if arg == "--open" {
			return true
		}
		if arg == "--background" {
			background = true
		}
	}
	return !background
}

func platformOpenDashboard(port int, adminKey string) error {
	u := url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port), Path: "/"}
	q := u.Query()
	q.Set("key", adminKey)
	u.RawQuery = q.Encode()
	file, err := windows.UTF16PtrFromString(u.String())
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
