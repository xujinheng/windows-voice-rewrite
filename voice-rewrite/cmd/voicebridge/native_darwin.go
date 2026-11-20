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
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	"voicerewrite/internal/core/audio"
	"voicerewrite/internal/core/clipboard"
	coreconfig "voicerewrite/internal/core/config"
	"voicerewrite/internal/core/indicator"
	"voicerewrite/internal/core/ux"
	"voicerewrite/voice"
)

var nativeConfig atomic.Pointer[coreconfig.Config]

// Physical state lives in the same Go owner used by Windows' mouse hook.
// Quartz callbacks are serial; Epoch is shared with the worker thread.
var nativeCore voice.Core
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
	if nativeCore.Down(int(code)) {
		return 1
	}
	return 0
}

//export VRPress
func VRPress(code C.int, busy C.int) C.ulonglong {
	if VRAction(code) == 0 {
		return 0
	}
	return C.ulonglong(nativeCore.Press(int(code), busy != 0))
}

//export VRActivate
func VRActivate(code C.int, token C.ulonglong) C.int {
	if nativeCore.Activate(int(code), uint64(token)) {
		return 1
	}
	return 0
}

//export VRRelease
func VRRelease(code C.int) C.int {
	return C.int(nativeCore.Release(int(code)))
}

//export VRAbandon
func VRAbandon() {
	nativeCore.Abandon() // retain key-up after recording timeout
}

//export VRArmed
func VRArmed() C.int {
	if nativeCore.Armed() {
		return 1
	}
	return 0
}

//export VREpochStart
func VREpochStart() C.ulonglong { return C.ulonglong(nativeCore.Start()) }

//export VREpochValid
func VREpochValid(token C.ulonglong) C.int {
	if nativeCore.Valid(uint64(token)) {
		return 1
	}
	return 0
}

//export VREpochCancel
func VREpochCancel() {
	nativeCore.Cancel()
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

//export VRLatestResult
func VRLatestResult() *C.char {
	entries := nativeCore.History.Snapshot()
	if len(entries) == 0 {
		return nil
	}
	return C.CString(entries[0].Text)
}

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
	reply := headlessProfile(json.RawMessage(C.GoString(config)))
	if reply.OK {
		nativeConfig.Store(&reply.cfg)
		profile := reply.Result.(map[string]any)["profile"].(map[string]any)
		nativeRole = [105]uint8{}
		nativeRole[macKeyCode(profile["dictation"].(string))] = 1
		nativeRole[macKeyCode(profile["rewrite"].(string))] = 2
	}
	return nativeJSON(reply)
}

const maxPCMBytes = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax

//export VRRun
func VRRun(rewrite C.int, audio *C.uchar, length C.int, selection *C.char, token C.ulonglong, onTranscribed C.VRTranscribedCallback) (result *C.char) {
	if length < 0 || length > C.int(maxPCMBytes) || (length > 0 && audio == nil) {
		return nil
	}
	cfg := nativeConfig.Load()
	if cfg == nil {
		return nativeJSON(failure("invalid_config", msgInvalidConfig))
	}
	selected := ""
	if selection != nil {
		selected = C.GoString(selection)
	}
	if rewrite != 0 && (len(selected) > 1<<20 || !utf8.ValidString(selected)) {
		return nativeJSON(failure("invalid_request", "selected text exceeds limits"))
	}
	var afterASR func(string)
	if onTranscribed != nil {
		afterASR = func(string) { C.vrReportTranscribed(onTranscribed, C.ulonglong(token)) }
	}
	ctx := nativeCore.ContextFor(uint64(token))
	// Native ABI errors and stale task results cannot escape into Swift.
	defer func() {
		if recover() != nil {
			result = nativeJSON(failure("internal_error", "request failed"))
		}
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				result = nativeJSON(failure("timeout", "request timed out"))
			} else {
				result = nativeJSON(failure("canceled", "request canceled"))
			}
		}
	}()
	outcome, err := nativeCore.Execute(uint64(token), *cfg, C.GoBytes(unsafe.Pointer(audio), length), selected, rewrite != 0, afterASR)
	if err == nil {
		return nativeJSON(success(outcome))
	}
	var pcm *voice.PCMError
	if errors.As(err, &pcm) {
		return nativeJSON(failure("invalid_pcm", pcm.Error()))
	}
	code, message := "asr_failed", "speech transcription failed"
	if rewrite != 0 {
		code, message = "textedit_failed", "selected text processing failed"
	}
	if classified, ok := ux.Classify(err); ok {
		message = classified
	}
	return nativeJSON(failure(code, message))
}
