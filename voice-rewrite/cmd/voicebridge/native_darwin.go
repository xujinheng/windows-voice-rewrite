//go:build darwin && cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef void (*VRTranscribedCallback)(unsigned long long);
static inline void vrReportTranscribed(VRTranscribedCallback fn, unsigned long long token) { fn(token); }
*/
import "C"

import (
	"encoding/json"
	"time"
	"unsafe"

	"voicerewrite/internal/core/clipboard"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/session"
)

var nativeEpoch session.Epoch

// Physical state lives in the same Go owner used by Windows' mouse hook.
// Quartz callbacks are serial; Epoch is shared with the worker thread.
var nativeHolds = input.NewHolds(105)
var nativeRole [105]uint8

//export VRAction
func VRAction(code C.int) C.int {
	if code < 0 || int(code) >= len(nativeRole) {
		return 0
	}
	return C.int(nativeRole[int(code)])
}

//export VRDown
func VRDown(code C.int) C.int {
	if nativeHolds.Down(int(code)) {
		return 1
	}
	return 0
}

//export VRPress
func VRPress(code C.int, busy C.int) C.ulonglong {
	if VRAction(code) == 0 {
		return 0
	}
	return C.ulonglong(nativeHolds.Press(int(code), busy != 0))
}

//export VRActivate
func VRActivate(code C.int, token C.ulonglong) C.int {
	if nativeHolds.Activate(int(code), uint64(token)) {
		return 1
	}
	return 0
}

//export VRRelease
func VRRelease(code C.int) C.int {
	held, active, primary := nativeHolds.Release(int(code))
	if !held {
		return 0
	}
	if active {
		return 2
	}
	if primary {
		return 3 // released an armed shortcut before its activation timer
	}
	return 1 // an unrelated side button should still navigate
}

//export VRAbandon
func VRAbandon() {
	nativeHolds.Abandon() // retain key-up after recording timeout
}

//export VRArmed
func VRArmed() C.int {
	if nativeHolds.Armed() {
		return 1
	}
	return 0
}

//export VREpochStart
func VREpochStart() C.ulonglong { return C.ulonglong(nativeEpoch.Start()) }

//export VREpochValid
func VREpochValid(token C.ulonglong) C.int {
	if nativeEpoch.Valid(uint64(token)) {
		return 1
	}
	return 0
}

//export VREpochCancel
func VREpochCancel() {
	nativeEpoch.Start()
	// Disabled event tap: release every hold and obsolete timer.
	nativeHolds.Cancel()
}

//export VRClipboardSnapshotFits
func VRClipboardSnapshotFits(size, total C.ulonglong) C.int {
	if clipboard.SnapshotFits(uintptr(size), uintptr(total)) {
		return 1
	}
	return 0
}

//export VRLevel
func VRLevel(previous, raw C.double) C.double {
	return C.double(indicator.SmoothedLevel(float64(previous), indicator.NormalizeMicrophoneLevel(float64(raw))))
}

//export VRWave
func VRWave(index C.int, level C.double, tick C.uint) C.int {
	return C.int(indicator.WaveHeight(int(index), float64(level), uint32(tick)))
}

//export VRSpinner
func VRSpinner(index C.int, tick C.uint) C.uint {
	x, y, alpha := indicator.SpinnerDot(int(index), uint32(tick))
	return C.uint(uint32(x)&255 | (uint32(y)&255)<<8 | uint32(alpha)<<16)
}

//export VRDuration
func VRDuration(seconds C.longlong, buffer *C.char, capacity C.int) {
	if buffer == nil || capacity <= 0 || capacity > 64 {
		return
	}
	value := indicator.DurationText(time.Unix(0, 0), time.Unix(int64(seconds), 0))
	dst := unsafe.Slice((*byte)(unsafe.Pointer(buffer)), int(capacity))
	n := copy(dst[:len(dst)-1], value)
	dst[n] = 0
}

//export VRFree
func VRFree(value unsafe.Pointer) { C.free(value) }

func nativeJSON(value response) *C.char {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return C.CString(string(data))
}

//export VRProfile
func VRProfile(config *C.char) *C.char {
	if config == nil {
		return nil
	}
	reply := headlessProfile(request{Config: json.RawMessage(C.GoString(config))})
	if reply.OK {
		profile := reply.Result.(map[string]any)["profile"].(map[string]any)
		nativeRole = [105]uint8{}
		nativeRole[macKeyCode(profile["dictation"].(string))] = 1
		nativeRole[macKeyCode(profile["rewrite"].(string))] = 2
	}
	return nativeJSON(reply)
}

//export VRRun
func VRRun(config *C.char, rewrite C.int, audio *C.uchar, length C.int, selection *C.char, token C.ulonglong, onTranscribed C.VRTranscribedCallback) *C.char {
	if config == nil || length < 0 || length > C.int(maxPCMBytes) || (length > 0 && audio == nil) {
		return nil
	}
	req := request{
		Config: json.RawMessage(C.GoString(config)), Rewrite: rewrite != 0,
		NativePCM: C.GoBytes(unsafe.Pointer(audio), length),
	}
	if selection != nil {
		req.SelectedText = C.GoString(selection)
	}
	if onTranscribed != nil {
		req.AfterASR = func(string) { C.vrReportTranscribed(onTranscribed, C.ulonglong(token)) }
	}
	// The shared voice.Run applies the task deadline; Epoch owns cancellation.
	return nativeJSON(safeProcessRequest(nativeEpoch.ContextFor(uint64(token)), req))
}
