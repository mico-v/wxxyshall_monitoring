//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmDestroy       = 0x0002
	wmNull          = 0x0000
	wmQueryEnd      = 0x0011
	wmContextMenu   = 0x007B
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmApp           = 0x8000
	wmTray          = wmApp + 1

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfChecked   = 0x00000008

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080

	trayOpenDashboard = 1001
	trayOpenData      = 1002
	trayOpenLog       = 1003
	trayAutostart     = 1004
	trayExit          = 1005
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadIcon         = user32.NewProc("LoadIconW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procPostMessage      = user32.NewProc("PostMessageW")
	procMessageBox       = user32.NewProc("MessageBoxW")
	procShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")

	trayPort     int
	trayAdminKey string
	traySignal   chan<- os.Signal
)

func platformReportError(title string, err error) {
	if err == nil {
		return
	}
	titleText, _ := windows.UTF16PtrFromString(title)
	messageText, _ := windows.UTF16PtrFromString(err.Error())
	procMessageBox.Call(0, uintptr(unsafe.Pointer(messageText)), uintptr(unsafe.Pointer(titleText)), 0x10)
}

type winPoint struct{ X, Y int32 }

type winMessage struct {
	HWnd     windows.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

type wndClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  windows.Handle
}

type notifyIconData struct {
	CbSize          uint32
	HWnd            windows.Handle
	UID             uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            windows.Handle
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            windows.GUID
	BalloonIcon     windows.Handle
}

func platformStartUI(port int, adminKey string, signalCh chan<- os.Signal) error {
	ready := make(chan error, 1)
	go runWindowsTray(port, adminKey, signalCh, ready)
	return <-ready
}

func runWindowsTray(port int, adminKey string, signalCh chan<- os.Signal, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	trayPort, trayAdminKey, traySignal = port, adminKey, signalCh

	className, _ := windows.UTF16PtrFromString("WxxyshallMonitoringTray")
	windowName, _ := windows.UTF16PtrFromString("宿舍电费监控")
	instanceValue, _, callErr := procGetModuleHandle.Call(0)
	if instanceValue == 0 {
		ready <- fmt.Errorf("获取模块句柄失败: %v", callErr)
		return
	}
	instance := windows.Handle(instanceValue)
	// 资源 ID 1 由 icon_windows_amd64.syso 嵌入，源文件是网页共用的
	// internal/web/static/favicon.ico。加载失败时回退到系统应用图标。
	icon, _, _ := procLoadIcon.Call(uintptr(instance), 1)
	if icon == 0 {
		icon, _, _ = procLoadIcon.Call(0, 32512)
	}
	wc := wndClassEx{
		CbSize:    uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(windowsTrayWndProc),
		Instance:  instance,
		Icon:      windows.Handle(icon),
		ClassName: className,
		IconSmall: windows.Handle(icon),
	}
	if registered, _, callErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); registered == 0 {
		ready <- fmt.Errorf("注册托盘窗口失败: %v", callErr)
		return
	}
	hwnd, _, callErr := procCreateWindowEx.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)), 0,
		0, 0, 0, 0, 0, 0, uintptr(instance), 0,
	)
	if hwnd == 0 {
		ready <- fmt.Errorf("创建托盘窗口失败: %v", callErr)
		return
	}

	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: windows.Handle(hwnd), UID: 1,
		Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTray, Icon: windows.Handle(icon),
	}
	copy(nid.Tip[:], windows.StringToUTF16("宿舍电费监控"))
	if ok, _, callErr := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ok == 0 {
		_, _, _ = procDestroyWindow.Call(hwnd)
		ready <- fmt.Errorf("添加系统托盘图标失败: %v", callErr)
		return
	}
	ready <- nil
	defer procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	var msg winMessage
	for {
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func windowsTrayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTray:
		event := uint32(lParam & 0xffff)
		if event == wmLButtonDblClk {
			_ = platformOpenDashboard(trayPort, trayAdminKey)
		} else if event == wmRButtonUp || event == wmContextMenu {
			showWindowsTrayMenu(hwnd)
		}
		return 0
	case wmQueryEnd:
		select {
		case traySignal <- os.Interrupt:
		default:
		}
		return 1
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func showWindowsTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendWindowsMenu(menu, mfString, trayOpenDashboard, "打开仪表盘 / 管理页面")
	appendWindowsMenu(menu, mfString, trayOpenData, "打开配置目录")
	appendWindowsMenu(menu, mfString, trayOpenLog, "打开运行日志")
	autostartFlags := uintptr(mfString)
	if windowsAutostartEnabled() {
		autostartFlags |= mfChecked
	}
	appendWindowsMenu(menu, autostartFlags, trayAutostart, "开机自动启动")
	procAppendMenu.Call(menu, mfSeparator, 0, 0)
	appendWindowsMenu(menu, mfString, trayExit, "退出")

	var point winPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	procSetForeground.Call(hwnd)
	command, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNonotify,
		uintptr(point.X), uintptr(point.Y), 0, hwnd, 0)
	switch command {
	case trayOpenDashboard:
		_ = platformOpenDashboard(trayPort, trayAdminKey)
	case trayOpenData:
		_ = openWindowsPath(dataDir())
	case trayOpenLog:
		_ = openWindowsPath(filepath.Join(dataDir(), "elec.log"))
	case trayAutostart:
		if windowsAutostartEnabled() {
			_ = unregisterWindowsStartup()
		} else {
			_ = registerWindowsStartup(filepath.Join(elecDir(), "elec.exe"))
		}
	case trayExit:
		select {
		case traySignal <- os.Interrupt:
		default:
		}
		procDestroyWindow.Call(hwnd)
	}
	procPostMessage.Call(hwnd, wmNull, 0, 0)
}

func appendWindowsMenu(menu, flags, id uintptr, label string) {
	text, _ := windows.UTF16PtrFromString(label)
	procAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
}

func openWindowsPath(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
