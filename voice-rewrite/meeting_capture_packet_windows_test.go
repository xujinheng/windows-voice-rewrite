//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"voicerewrite/internal/core/audio"
)

func TestMeetingCaptureCancelledBeforeOpeningWASAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := captureMeetingAudio(ctx, 0, func() {
		t.Fatal("started a canceled capture")
	}, func([]byte) {
		t.Fatal("emitted audio from a canceled capture")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture error = %v, want context cancellation", err)
	}
}

// A native IAudioCaptureClient vtable verifies GetBuffer/ReleaseBuffer pairing
// without requiring a live speaker or conferencing process on the CI runner.
func TestMeetingPacketReleasesNativeBuffer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		frames    uint32
		flags     uint32
		data      bool
		wantError bool
		wantOut   int
	}{
		{"PCM", 882, 0, true, false, 1},
		{"silent", 882, 2, false, false, 1},
		{"missing-data", 882, 0, false, true, 0},
		{"oversized", 44100 * 11, 0, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			converter, err := audio.NewPCMConverter(audio.PCMFormat{
				SampleRate: 44100, Channels: 2, BitsPerSample: 16,
			})
			if err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 882*4)
			var dataPtr uintptr
			if tc.data {
				dataPtr = uintptr(unsafe.Pointer(&data[0]))
			}
			var released int
			var vtable [5]uintptr
			vtable[3] = syscall.NewCallback(func(_self, raw, count, flags, _pos, _qpc uintptr) uintptr {
				*(*uintptr)(unsafe.Pointer(raw)) = dataPtr
				*(*uint32)(unsafe.Pointer(count)) = tc.frames
				*(*uint32)(unsafe.Pointer(flags)) = tc.flags
				return 0
			})
			vtable[4] = syscall.NewCallback(func(_self, count uintptr) uintptr {
				released++
				if count != uintptr(tc.frames) {
					t.Errorf("ReleaseBuffer(%d), want %d", count, tc.frames)
				}
				return 0
			})
			capture := struct{ vtable uintptr }{uintptr(unsafe.Pointer(&vtable[0]))}
			var output [][]byte
			frames, err := readMeetingPacket(uintptr(unsafe.Pointer(&capture)), converter, func(frame []byte) {
				output = append(output, append([]byte(nil), frame...))
			})
			runtime.KeepAlive(data)
			runtime.KeepAlive(vtable)
			runtime.KeepAlive(capture)
			if frames != tc.frames || (err != nil) != tc.wantError {
				t.Fatalf("read: frames=%d error=%v; want %d, error=%v", frames, err, tc.frames, tc.wantError)
			}
			if released != 1 {
				t.Fatalf("ReleaseBuffer called %d times; want one", released)
			}
			if len(output) != tc.wantOut {
				t.Fatalf("emitted %d frames; want %d", len(output), tc.wantOut)
			}
			if tc.flags&2 != 0 && len(output) != 0 && !bytes.Equal(output[0], make([]byte, audio.FrameBytes)) {
				t.Fatal("silent WASAPI packet emitted nonzero samples")
			}
		})
	}
}
