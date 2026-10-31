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
	mapvkVkToVsc = 0
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
	// Tab/Enter/Backspace меняют фокус или текст и не подходят для текстовых замен.
	return vk == 0x20 || (vk >= 0x30 && vk <= 0x39) ||
		(vk >= 0x41 && vk <= 0x5A) || (vk >= 0x60 && vk <= 0x6F) ||
		(vk >= 0xBA && vk <= 0xC0) || (vk >= 0xDB && vk <= 0xDF) || vk == 0xE2
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
	var k registry.Key
	var err error
	if on {
		k, _, err = registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	} else {
		k, err = registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err == registry.ErrNotExist {
			return nil
		}
	}
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
	command := syscall.EscapeArg(exe)
	if configOverride != "" {
		command += " --config " + syscall.EscapeArg(configOverride)
	}
	return k.SetStringValue(runValueName, command)
}

func triggerName(vk uint16, shift bool) string {
	name := keyName(vk)
	if shift {
		return "Shift + " + name
	}
	return name
}

func isModifier(vk uint16) bool {
	switch vk {
	case 0x10, 0x11, 0x12, 0x5B, 0x5C, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

func layoutName(code uint16) string {
	switch code {
	case 0:
		return "Любая раскладка"
	case 0x409:
		return "Английская (США)"
	case 0x419:
		return "Русская"
	case 0x809:
		return "Английская (Великобритания)"
	}
	return fmt.Sprintf("Раскладка %04X", code)
}

func layoutChoices() ([]string, []uint16) {
	options, codes := []string{layoutName(0)}, []uint16{0}
	proc := user32.NewProc("GetKeyboardLayoutList")
	n, _, _ := proc.Call(0, 0)
	if n == 0 {
		return options, codes
	}
	layouts := make([]uintptr, n)
	n, _, _ = proc.Call(n, uintptr(unsafe.Pointer(&layouts[0])))
	seen := map[uint16]bool{0: true}
	for _, layout := range layouts[:n] {
		code := uint16(layout)
		if !seen[code] {
			options, codes = append(options, layoutName(code)), append(codes, code)
			seen[code] = true
		}
	}
	return options, codes
}
