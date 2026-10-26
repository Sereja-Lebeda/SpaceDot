package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procMapVirtualKeyW  = user32.NewProc("MapVirtualKeyW")
	procGetKeyNameTextW = user32.NewProc("GetKeyNameTextW")
)

const (
	mapvkVkToVsc  = 0
	mapvkVkToChar = 2
)

// Клавиши, для которых GetKeyNameText нужен флаг «расширенная клавиша»
var extendedKeys = map[uint16]bool{
	0x21: true, 0x22: true, 0x23: true, 0x24: true, // PgUp, PgDn, End, Home
	0x25: true, 0x26: true, 0x27: true, 0x28: true, // стрелки
	0x2D: true, 0x2E: true, // Insert, Delete
	0x5B: true, 0x5C: true, 0x5D: true, // Win, Menu
	0x6F: true, 0x90: true, // Num /, NumLock
	0xA3: true, 0xA5: true, // правые Ctrl, Alt
}

// keyName возвращает понятное название клавиши по её virtual-key коду
func keyName(vk uint16) string {
	scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapvkVkToVsc)
	lparam := scan << 16
	if extendedKeys[vk] {
		lparam |= 1 << 24
	}
	buf := make([]uint16, 64)
	n, _, _ := procGetKeyNameTextW.Call(lparam, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return fmt.Sprintf("Клавиша %d", vk)
	}
	return syscall.UTF16ToString(buf[:n])
}

// keyTypesChar сообщает, печатает ли клавиша символ (пробел, буквы, цифры…),
// чтобы знать, сколько символов стирать перед заменой
func keyTypesChar(vk uint16) bool {
	ch, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapvkVkToChar)
	return ch != 0
}

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "SpaceDot"
)

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if !on {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, `"`+exe+`"`)
}
