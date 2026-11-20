//go:build windows

package main

import (
	"unsafe"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/desktop"
)

func uxMonitorForPoint(pt trayPoint) uintptr {
	packed := uint64(uint32(pt.X)) | uint64(uint32(pt.Y))<<32
	mon, _, _ := procMonitorFromPointHandy.Call(uintptr(packed), MONITOR_DEFAULTTONEAREST)
	return mon
}
func uxWorkAreaForMonitor(mon uintptr) (desktop.WorkArea, bool) {
	if mon == 0 {
		return desktop.WorkArea{}, false
	}
	info := handyMonitorInfo{Size: uint32(unsafe.Sizeof(handyMonitorInfo{}))}
	if ok, _, _ := procGetMonitorInfoHandy.Call(mon, uintptr(unsafe.Pointer(&info))); ok != 0 {
		return desktop.WorkArea{Left: info.Work.Left, Top: info.Work.Top, Right: info.Work.Right, Bottom: info.Work.Bottom}, true
	}
	return desktop.WorkArea{}, false
}

func uxPrimaryScreen() desktop.WorkArea {
	w, _, _ := procGetSystemMetrics.Call(0)
	h, _, _ := procGetSystemMetrics.Call(1)
	return desktop.WorkArea{Left: 0, Top: 0, Right: int32(w), Bottom: int32(h)}
}

func uxWorkAreaForPoint(pt trayPoint) desktop.WorkArea {
	if area, ok := uxWorkAreaForMonitor(uxMonitorForPoint(pt)); ok {
		return area
	}
	return uxPrimaryScreen()
}
func uxMonitorScaleForPoint(pt trayPoint) float64 { return handyScaleForMonitor(uxMonitorForPoint(pt)) }
