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

// One fixed physical-key table contains both the Go-validated role and the
// shared Hold state. Quartz callbacks are serial; Epoch also serves workers.
var nativeKeys [105]struct {
	hold input.Hold
	role uint8
}
var nativeKey int

//export VRAction
func VRAction(code C.int) C.int {
	if code < 0 || int(code) >= len(nativeKeys) {
		return 0
	}
	return C.int(nativeKeys[int(code)].role)
}

func nativeHold(code int) *input.Hold {
	if code < 0 || code >= len(nativeKeys) {
		return nil
	}
	return &nativeKeys[code].hold
}

//export VRDown
func VRDown(code C.int) C.int {
	if hold := nativeHold(int(code)); hold != nil && hold.Down() {
		return 1
	}
	return 0
}

//export VRPress
func VRPress(code C.int, busy C.int) C.ulonglong {
	if VRAction(code) == 0 || nativeKey != 0 || busy != 0 {
		return 0 // unbound or busy input does not acquire a physical hold
	}
	h := nativeHold(int(code))
	token, ok := h.Press()
	if !ok {
		return 0
	}
	nativeKey = int(code)
	return C.ulonglong(token)
}

//export VRActivate
func VRActivate(code C.int, token C.ulonglong) C.int {
	if nativeKey == int(code) && nativeHold(int(code)).Activate(uint64(token)) {
		return 1
	}
	return 0
}

//export VRRelease
func VRRelease(code C.int) C.int {
	hold := nativeHold(int(code))
	if hold == nil {
		return 0
	}
	held, active := hold.Release()
	primary := nativeKey == int(code)
	if primary {
		nativeKey = 0
	}
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
	nativeKey = 0 // physical hold remains, so a short side click can replay
}

//export VRArmed
func VRArmed() C.int {
	if nativeKey != 0 {
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
	nativeKey = 0
	// A disabled event tap can lose the physical key-up event. Release the
	// existing Hold objects; do not replace them, or old timer tokens can
	// collide with a later press.
	for i := range nativeKeys {
		nativeKeys[i].hold.Release()
	}
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
		for i := range nativeKeys {
			nativeKeys[i].role = 0
		}
		nativeKeys[macKeyCode(profile["dictation"].(string))].role = 1
		nativeKeys[macKeyCode(profile["rewrite"].(string))].role = 2
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
