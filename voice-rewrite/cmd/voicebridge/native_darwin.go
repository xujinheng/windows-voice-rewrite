//go:build darwin && cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"time"
	"unsafe"

	"voicerewrite/internal/core/clipboard"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/input"
	"voicerewrite/internal/core/session"
	"voicerewrite/internal/core/ux"
)

var nativeEpoch session.Epoch

// The physical key state is the existing shared input.Hold model, not a
// second Swift press/arm/cancel state machine. Quartz callbacks run on the
// main thread; only Epoch is accessed from both the UI and worker threads.
var nativeHolds = map[int]*input.Hold{}
var nativeKey int

func nativeHold(code int) *input.Hold {
	h := nativeHolds[code]
	if h == nil {
		h = &input.Hold{}
		nativeHolds[code] = h
	}
	return h
}

//export VRDown
func VRDown(code C.int) C.int {
	if nativeHold(int(code)).Down() {
		return 1
	}
	return 0
}

//export VRPress
func VRPress(code C.int, busy C.int) C.ulonglong {
	h := nativeHold(int(code))
	token, ok := h.Press()
	if !ok || nativeKey != 0 || busy != 0 {
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
	held, active := nativeHold(int(code)).Release()
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
	for _, hold := range nativeHolds {
		hold.Release()
	}
}

//export VRReady
func VRReady(op C.int, token C.ulonglong, focusOK, sensitive, selectionOK C.int) C.int {
	password := func() bool { return sensitive != 0 }
	windowOK := func() bool { return focusOK != 0 }
	keysUp := func() bool { return nativeKey == 0 }
	var err error
	switch int(op) {
	case 0:
		err = ux.SelectionReadReady(focusOK != 0, password)
	case 1:
		err = ux.DictationTargetReady(keysUp, func() bool { return nativeEpoch.Valid(uint64(token)) && windowOK() }, password)
	case 2:
		err = ux.ReplaceInsertReady(func() bool { return nativeEpoch.Valid(uint64(token)) }, keysUp,
			func() bool { return windowOK() && selectionOK != 0 }, password)
	default:
		return 0
	}
	if err == nil {
		return 1
	}
	return 0
}

//export VRClipboardFits
func VRClipboardFits(size, total C.longlong) C.int {
	if size > 0 && total >= 0 && clipboard.SnapshotFits(uintptr(size), uintptr(total)) {
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
	return nativeJSON(headlessProfile(request{ID: "host", Config: json.RawMessage(C.GoString(config))}))
}

//export VRRun
func VRRun(config, operation *C.char, audio *C.uchar, length C.int, selection *C.char, token C.ulonglong) *C.char {
	if config == nil || operation == nil || length < 0 || length > C.int(maxPCMBytes) || (length > 0 && audio == nil) {
		return nil
	}
	req := request{
		ID: "native", Config: json.RawMessage(C.GoString(config)), Op: C.GoString(operation),
		CanInsert: true, NativePCM: C.GoBytes(unsafe.Pointer(audio), length),
	}
	if selection != nil {
		req.SelectedText = C.GoString(selection)
	}
	// Share the CLI's 220s ASR / 240s selected-text deadlines, even when
	// Swift never launches a helper. Epoch invalidation remains the parent.
	ctx, cancel := context.WithTimeout(nativeEpoch.ContextFor(uint64(token)), defaultOptions().timeoutFor(req))
	defer cancel()
	return nativeJSON(safeProcessRequest(ctx, req))
}
