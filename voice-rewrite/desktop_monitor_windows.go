//go:build windows

package main

import (
	"unsafe"

	"voicerewrite/internal/core/desktop"
)

func uxMonitorForPoint(pt trayPoint) uintptr {
	packed := uint64(uint32(pt.X)) | uint64(uint32(pt.Y))<<32
	mon, _, _ := procMonitorFromPointHandy.Call(uintptr(packed), MONITOR_DEFAULTTONEAREST)
	return mon
}
func uxWorkAreaForPoint(pt trayPoint) desktop.WorkArea {
	mon := uxMonitorForPoint(pt)
	if mon != 0 {
		info := handyMonitorInfo{Size: uint32(unsafe.Sizeof(handyMonitorInfo{}))}
		if ok, _, _ := procGetMonitorInfoHandy.Call(mon, uintptr(unsafe.Pointer(&info))); ok != 0 {
			return desktop.WorkArea{info.Work.Left, info.Work.Top, info.Work.Right, info.Work.Bottom}
		}
	}
	w, _, _ := procGetSystemMetrics.Call(0)
	h, _, _ := procGetSystemMetrics.Call(1)
	return desktop.WorkArea{0, 0, int32(w), int32(h)}
}
func uxMonitorScaleForPoint(pt trayPoint) float64 { return handyScaleForMonitor(uxMonitorForPoint(pt)) }
