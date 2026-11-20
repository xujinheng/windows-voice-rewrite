//go:build darwin && cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>
// Only the C ABI shapes are duplicated here for cgo; Core rules are in Go.
// cgo exports non-const pointers, while the Swift header accepts const inputs.
typedef struct { int kind; uint64_t token; int role; int release; } VRCommandEvent;
typedef struct {
    int role, held_role, down, armed, can_activate, valid;
    uint64_t current;
    int phase, closed;
} VRCoreState;
typedef void (*VREventCallback)(int kind, uint64_t token);
static inline void vrReportEvent(VREventCallback fn, int kind, uint64_t token) { fn(kind, token); }
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

	coreconfig "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/audio"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/clipboard"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/indicator"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/ux"
	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/voice"
)

var nativeConfig atomic.Pointer[coreconfig.Config]

// Quartz and Win32 both use the same command receipt and immutable Core state.
var nativeCore voice.Core

//export VRCommand
func VRCommand(kind, code C.int, token C.uint64_t, busy C.int) C.VRCommandEvent {
	e := nativeCore.Apply(voice.Command{
		Kind: voice.CommandKind(kind), Code: int(code), Token: uint64(token), Busy: busy != 0,
	})
	return C.VRCommandEvent{kind: C.int(e.Kind), token: C.uint64_t(e.Token),
		role: C.int(e.Role), release: C.int(e.Release)}
}

//export VRSnapshot
func VRSnapshot(code C.int, token C.uint64_t) C.VRCoreState {
	s := nativeCore.Snapshot(int(code), uint64(token))
	r := C.VRCoreState{role: C.int(s.Role), held_role: C.int(s.HeldRole),
		current: C.uint64_t(s.Current), phase: C.int(s.Phase)}
	if s.Down {
		r.down = 1
	}
	if s.Armed {
		r.armed = 1
	}
	if s.CanActivate {
		r.can_activate = 1
	}
	if s.Valid {
		r.valid = 1
	}
	if s.Closed {
		r.closed = 1
	}
	return r
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
		nativeCore.Apply(voice.Command{Kind: voice.CommandBind, Dictation: []int{macKeyCode(profile["dictation"].(string))}, Rewrite: []int{macKeyCode(profile["rewrite"].(string))}})
	}
	return nativeJSON(reply)
}

const maxPCMBytes = audio.PCMBytesPerSecond * coreconfig.RecordSecondsMax

//export VRRun
func VRRun(rewrite C.int, audio *C.uchar, length C.int, selection *C.char, token C.ulonglong, onEvent C.VREventCallback) (result *C.char) {
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
	var emit func(voice.Event)
	if onEvent != nil {
		emit = func(event voice.Event) {
			C.vrReportEvent(onEvent, C.int(event.Kind), C.uint64_t(event.Token))
		}
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
	outcome, err := nativeCore.Execute(uint64(token), *cfg, C.GoBytes(unsafe.Pointer(audio), length), selected, rewrite != 0, emit)
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
