package main

import (
	"testing"
	"time"
	"unsafe"
)

func TestWindowsHookLifecycle(t *testing.T) {
	events, stop, err := startInputHook()
	if err != nil {
		t.Fatal(err)
	}
	stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline.C:
			t.Fatal("hook thread did not exit")
		}
	}
}

func TestWindowsInputSize(t *testing.T) {
	want := uintptr(28)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 40
	}
	if got := unsafe.Sizeof(nativeInput{}); got != want {
		t.Fatalf("INPUT size %d, want %d", got, want)
	}
}

func TestOnlyPrintableTriggers(t *testing.T) {
	for _, key := range []uint16{8, 9, 13, 16, 17, 18, 27, 37, 38, 39, 40, 46, 112} {
		if keyTypesChar(key) {
			t.Fatalf("unsafe text trigger %d", key)
		}
	}
	for _, key := range []uint16{32, 48, 65, 96, 190, 191, 226} {
		if !keyTypesChar(key) {
			t.Fatalf("printable key rejected %d", key)
		}
	}
}
