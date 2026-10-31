package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetHook                  = user32.NewProc("SetWindowsHookExW")
	procCallNextHook             = user32.NewProc("CallNextHookEx")
	procUnhook                   = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage               = user32.NewProc("GetMessageW")
	procPeekMessage              = user32.NewProc("PeekMessageW")
	procPostThreadMessage        = user32.NewProc("PostThreadMessageW")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout        = user32.NewProc("GetKeyboardLayout")
	procGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	procSendInput                = user32.NewProc("SendInput")
)

const inputMarker = 0x53504454

type keyEvent struct {
	key                               uint16
	up, shift, blocked, reset, repeat bool
	window                            uintptr
	when                              time.Time
}
type lowLevelKey struct {
	VK, Scan, Flags, Time uint32
	Extra                 uintptr
}

// INPUT union выровнен по MOUSEINPUT; это сохраняет размер и на x86, и на x64.
type mouseInput struct {
	X, Y              int32
	Data, Flags, Time uint32
	Extra             uintptr
}
type nativeInput struct {
	Kind uint32
	Data mouseInput
}
type keyboardInput struct {
	VK, Scan    uint16
	Flags, Time uint32
	Extra       uintptr
}

func replaceText(erase int, text string) error {
	inputs := make([]nativeInput, 0, erase*2+len(text)*2)
	appendKey := func(vk, scan uint16, flags uint32) {
		input := nativeInput{Kind: 1}
		*(*keyboardInput)(unsafe.Pointer(&input.Data)) = keyboardInput{VK: vk, Scan: scan, Flags: flags, Extra: inputMarker}
		inputs = append(inputs, input)
	}
	for i := 0; i < erase; i++ {
		appendKey(8, 0, 0)
		appendKey(8, 0, 2)
	}
	for _, ch := range utf16.Encode([]rune(text)) {
		appendKey(0, ch, 4)
		appendKey(0, ch, 6)
	}
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	runtime.KeepAlive(inputs)
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput: %d/%d: %w", n, len(inputs), err)
	}
	return nil
}
func keyHeld(vk uintptr) bool {
	state, _, _ := procGetAsyncKeyState.Call(vk)
	return state&0x8000 != 0
}

// Только горутина listenKeys обращается к кешу foregroundContext.
var foregroundPID uint32
var foregroundApp string

func foregroundContext() inputContext {
	hwnd, _, _ := procGetForegroundWindow.Call()
	var pid uint32
	thread, _, _ := procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	layout, _, _ := procGetKeyboardLayout.Call(thread)
	context := inputContext{window: hwnd, layout: uint16(layout)}
	if pid == foregroundPID {
		context.app = foregroundApp
		return context
	}
	foregroundPID, foregroundApp = pid, ""
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err == nil {
		defer windows.CloseHandle(process)
		buf := make([]uint16, 32768)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(process, 0, &buf[0], &size) == nil {
			full := windows.UTF16ToString(buf[:size])
			full = filepath.Base(full)
			foregroundApp = full
		}
	}
	context.app = foregroundApp
	return context
}
func startInputHook() (<-chan keyEvent, func(), error) {
	events := make(chan keyEvent, 512)
	ready := make(chan error, 1)
	var threadID uint32
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		threadID = windows.GetCurrentThreadId()
		send := func(ev keyEvent) {
			select {
			case events <- ev:
			default:
				// При перегрузке не соединяем нажатия по разные стороны пропущенного ввода.
				for {
					select {
					case <-events:
						continue
					default:
						events <- keyEvent{reset: true}
						return
					}
				}
			}
		}
		callback := windows.NewCallback(func(code int, message uintptr, event *lowLevelKey) uintptr {
			if code >= 0 {
				if event.Extra != inputMarker {
					hwnd, _, _ := procGetForegroundWindow.Call()
					send(keyEvent{key: uint16(event.VK), up: message == 0x101 || message == 0x105,
						shift: keyHeld(0x10), blocked: keyHeld(0x11) || keyHeld(0x12) || keyHeld(0x5B) || keyHeld(0x5C),
						window: hwnd, when: time.Now()})
				}
			}
			result, _, _ := procCallNextHook.Call(0, uintptr(code), message, uintptr(unsafe.Pointer(event)))
			return result
		})
		mouseCallback := windows.NewCallback(func(code int, message uintptr, data uintptr) uintptr {
			if code >= 0 && (message == 0x201 || message == 0x204 || message == 0x207 || message == 0x20A || message == 0x20E) {
				send(keyEvent{reset: true})
			}
			result, _, _ := procCallNextHook.Call(0, uintptr(code), message, data)
			return result
		})
		keyboard, _, err := procSetHook.Call(13, callback, 0, 0)
		if keyboard == 0 {
			ready <- fmt.Errorf("keyboard hook: %w", err)
			return
		}
		defer procUnhook.Call(keyboard)
		mouse, _, err := procSetHook.Call(14, mouseCallback, 0, 0)
		if mouse == 0 {
			ready <- fmt.Errorf("mouse hook: %w", err)
			return
		}
		defer procUnhook.Call(mouse)
		var msg struct {
			Window  uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			X, Y    int32
			Private uint32
		}
		procPeekMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
		ready <- nil
		for {
			result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(result) <= 0 {
				break
			}
		}
		close(events)
	}()
	if err := <-ready; err != nil {
		return nil, nil, err
	}
	return events, func() { procPostThreadMessage.Call(uintptr(threadID), 0x12, 0, 0) }, nil
}
