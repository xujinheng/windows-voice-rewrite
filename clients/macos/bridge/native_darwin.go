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
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"
	"unsafe"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/audio"
	"github.com/xujinheng/windows-voice-rewrite/core/clipboard"
	"github.com/xujinheng/windows-voice-rewrite/core/indicator"
	"github.com/xujinheng/windows-voice-rewrite/core/localasr"
	"github.com/xujinheng/windows-voice-rewrite/core/settingsform"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
	"github.com/xujinheng/windows-voice-rewrite/core/voice"
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
		go func() { _ = localasr.Resume(context.Background(), reply.cfg, nil) }()
		profile := reply.Result.(map[string]any)["profile"].(map[string]any)
		nativeCore.Apply(voice.Command{Kind: voice.CommandBind, Dictation: []int{macKeyCode(profile["dictation"].(string))}, Rewrite: []int{macKeyCode(profile["rewrite"].(string))}})
	}
	return nativeJSON(reply)
}

//export VRASRProgress
func VRASRProgress() *C.char {
	p := localasr.ProgressSnapshot()
	return nativeJSON(success(map[string]any{"status": p.Text, "directory": p.Directory, "busy": p.Busy}))
}

//export VRDeploy
func VRDeploy(directory *C.char) *C.char {
	if directory == nil {
		return nativeJSON(failure("invalid_directory", "missing directory"))
	}
	if err := localasr.Deploy(context.Background(), C.GoString(directory), nil); err != nil {
		return nativeJSON(failure("deploy_failed", err.Error()))
	}
	return nativeJSON(success(map[string]string{"status": "ASR 已部署"}))
}

//export VRStopASR
func VRStopASR() { localasr.Stop() }

//export VRSettings
func VRSettings(request *C.char) *C.char {
	if request == nil {
		return nativeJSON(failure("invalid_request", "missing settings request"))
	}
	value, err := settingsform.ConnectionAction(context.Background(), []byte(C.GoString(request)))
	if err == nil {
		if output, ok := value.(map[string]any); ok {
			if _, isForm := output["form"]; isForm {
				capture := map[string]string{}
				for name, code := range macShortcutCodes {
					capture[strconv.Itoa(code)] = name
				}
				output["capture"] = capture
			}
		}
		if saved, ok := value.(map[string]string); ok {
			if raw, isSave := saved["config"]; isSave {
				profile := headlessProfile(json.RawMessage(raw))
				if !profile.OK {
					return nativeJSON(failure("invalid_config", "请选择当前 macOS 支持的不同快捷键"))
				}
				data, marshalErr := coreconfig.Marshal(profile.cfg)
				if marshalErr != nil {
					return nativeJSON(failure("settings_failed", marshalErr.Error()))
				}
				saved["config"] = string(data)
			}
		}
	}
	if err != nil {
		message := err.Error()
		if friendly, ok := ux.Classify(err); ok {
			message = friendly
		}
		return nativeJSON(failure("settings_failed", message))
	}
	return nativeJSON(success(value))
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
	mode := voice.TaskDictation
	if rewrite != 0 {
		mode = voice.TaskRewrite
	}
	outcome, err := nativeCore.Execute(ctx, voice.TaskRequest{Token: uint64(token), Config: *cfg, PCM: C.GoBytes(unsafe.Pointer(audio), length), SelectedText: selected, Mode: mode}, emit)
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
