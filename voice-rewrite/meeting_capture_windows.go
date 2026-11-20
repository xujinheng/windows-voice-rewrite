//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"voicerewrite/internal/core/audio"
)

var (
	ole32                      = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx             = ole32.NewProc("CoInitializeEx")
	coUninitialize             = ole32.NewProc("CoUninitialize")
	activateMeetingAudio       = syscall.NewLazyDLL("Mmdevapi.dll").NewProc("ActivateAudioInterfaceAsync")
	createMeetingMarshaler     = ole32.NewProc("CoCreateFreeThreadedMarshaler")
	createMeetingEvent         = kernel32.NewProc("CreateEventW")
	waitMeetingEvent           = kernel32.NewProc("WaitForSingleObject")
	meetingAudioInterface      = syscall.GUID{Data1: 0x1cb9ad4c, Data2: 0xdbfa, Data3: 0x4c32, Data4: [8]byte{0xb1, 0x78, 0xc2, 0xf5, 0x68, 0xa7, 0x03, 0xb2}}
	meetingCaptureInterface    = syscall.GUID{Data1: 0xc8adbd64, Data2: 0xe71e, Data3: 0x48a0, Data4: [8]byte{0xa4, 0xde, 0x18, 0x5c, 0x39, 0x5c, 0xd3, 0x17}}
	meetingUnknownInterface    = syscall.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	meetingAgileInterface      = syscall.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	meetingMarshalInterface    = syscall.GUID{Data1: 3, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	meetingCompletionInterface = syscall.GUID{Data1: 0x41d949ab, Data2: 0x9862, Data3: 0x444a, Data4: [8]byte{0x80, 0xf6, 0xc2, 0x61, 0x33, 0x4d, 0xa5, 0xeb}}
	meetingActivations         sync.Map // native object address -> *meetingActivation
	meetingActivationVTable    = [...]uintptr{
		syscall.NewCallback(meetingQueryInterface),
		syscall.NewCallback(meetingAddRef),
		syscall.NewCallback(meetingRelease),
		syscall.NewCallback(meetingActivateCompleted),
	}
)

//go:uintptrescapes
func comMethod(obj uintptr, index uintptr, args ...uintptr) uintptr {
	vt := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vt + index*unsafe.Sizeof(uintptr(0))))
	parameters := append([]uintptr{obj}, args...)
	hr, _, _ := syscall.SyscallN(fn, parameters...)
	return hr
}

func comFailed(hr uintptr) bool { return int32(hr) < 0 }

type meetingActivationBlob struct {
	kind, pid, mode uint32
}

type meetingActivationVariant struct {
	tag      uint16
	reserved [3]uint16
	size     uint32
	data     uintptr
}

// ASR capture is activated asynchronously by Windows. Native memory keeps
// the PROPVARIANT and its payload alive until the COM callback is released.
type meetingActivationObject struct {
	vtable uintptr
	params meetingActivationVariant
	blob   meetingActivationBlob
}

type meetingActivation struct {
	refs      atomic.Uint32
	marshaler uintptr
	mu        sync.Mutex
	abandoned bool
	done      chan meetingActivationResult
}

type meetingActivationResult struct {
	client uintptr
	err    error
}

func meetingQueryInterface(self, iid, out uintptr) uintptr {
	p := (*uintptr)(unsafe.Pointer(out))
	*p = 0
	id := *(*syscall.GUID)(unsafe.Pointer(iid))
	switch id {
	case meetingUnknownInterface, meetingCompletionInterface, meetingAgileInterface:
		// WASAPI probes IAgileObject separately from the free-threaded marshaler.
		*p = self
		meetingAddRef(self)
		return 0
	case meetingMarshalInterface:
		if value, ok := meetingActivations.Load(self); ok && value.(*meetingActivation).marshaler != 0 {
			return comMethod(value.(*meetingActivation).marshaler, 0, iid, out)
		}
	}
	return 0x80004002 // E_NOINTERFACE
}

func meetingAddRef(self uintptr) uintptr {
	value, _ := meetingActivations.Load(self)
	return uintptr(value.(*meetingActivation).refs.Add(1))
}

func meetingRelease(self uintptr) uintptr {
	value, _ := meetingActivations.Load(self)
	a := value.(*meetingActivation)
	refs := a.refs.Add(^uint32(0))
	if refs == 0 {
		meetingActivations.Delete(self)
		if a.marshaler != 0 {
			comMethod(a.marshaler, 2)
		}
		procGlobalFree.Call(self)
	}
	return uintptr(refs)
}

func meetingActivateCompleted(self, op uintptr) uintptr {
	value, _ := meetingActivations.Load(self)
	a := value.(*meetingActivation)
	var activated int32
	var unknown, client uintptr
	hr := comMethod(op, 3, uintptr(unsafe.Pointer(&activated)), uintptr(unsafe.Pointer(&unknown)))
	err := meetingCOMError("获取应用音频接口", hr)
	if err == nil {
		err = meetingCOMError("激活应用音频", uintptr(uint32(activated)))
	}
	if unknown != 0 {
		defer comMethod(unknown, 2)
		if err == nil {
			err = meetingCOMError("查询 IAudioClient", comMethod(unknown, 0, uintptr(unsafe.Pointer(&meetingAudioInterface)), uintptr(unsafe.Pointer(&client))))
		}
	}
	if err == nil && client == 0 {
		err = errors.New("Windows 未返回应用音频接口")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.abandoned {
		if client != 0 {
			comMethod(client, 2)
		}
	} else {
		a.done <- meetingActivationResult{client: client, err: err}
	}
	return 0
}

func meetingCOMError(action string, hr uintptr) error {
	if comFailed(hr) {
		return fmt.Errorf("%s失败（0x%08X）", action, uint32(hr))
	}
	return nil
}

func openMeetingAudio(ctx context.Context, pid uint32) (uintptr, error) {
	if err := activateMeetingAudio.Find(); err != nil {
		return 0, errors.New("当前 Windows 版本不支持按应用采集音频")
	}
	self, _, err := procGlobalAlloc.Call(0x0040, unsafe.Sizeof(meetingActivationObject{}))
	if self == 0 {
		return 0, fmt.Errorf("分配音频接口: %w", err)
	}
	a := &meetingActivation{done: make(chan meetingActivationResult, 1)}
	a.refs.Store(1)
	meetingActivations.Store(self, a)
	defer meetingRelease(self)

	object := (*meetingActivationObject)(unsafe.Pointer(self))
	object.vtable = uintptr(unsafe.Pointer(&meetingActivationVTable[0]))
	object.blob = meetingActivationBlob{kind: 1, pid: pid} // include target process tree
	object.params = meetingActivationVariant{tag: 65, size: uint32(unsafe.Sizeof(object.blob)), data: uintptr(unsafe.Pointer(&object.blob))}
	var marshaler uintptr
	hr, _, _ := createMeetingMarshaler.Call(self, uintptr(unsafe.Pointer(&marshaler)))
	if err := meetingCOMError("创建音频回调", hr); err != nil {
		return 0, err
	}
	a.marshaler = marshaler

	device, _ := syscall.UTF16PtrFromString(`VAD\Process_Loopback`)
	var op uintptr
	hr, _, _ = activateMeetingAudio.Call(uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&meetingAudioInterface)), uintptr(unsafe.Pointer(&object.params)), self, uintptr(unsafe.Pointer(&op)))
	runtime.KeepAlive(device)
	if err := meetingCOMError("打开应用音频", hr); err != nil {
		return 0, err
	}
	if op != 0 {
		defer comMethod(op, 2)
	}

	select {
	case result := <-a.done:
		return result.client, result.err
	case <-ctx.Done():
		a.mu.Lock()
		a.abandoned = true
		select {
		case result := <-a.done:
			if result.client != 0 {
				comMethod(result.client, 2)
			}
		default:
		}
		a.mu.Unlock()
		return 0, ctx.Err()
	}
}

func captureMeetingAudio(ctx context.Context, pid uint32, started func(), push func([]byte)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, _, _ := meetingOpenProcess.Call(0x00100000, 0, uintptr(pid)) // SYNCHRONIZE
	if target == 0 {
		return errors.New("选中的音频来源已退出或不可访问")
	}
	defer procCloseHandle.Call(target)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitializeEx.Call(0, 0)
	if err := meetingCOMError("初始化 Windows 音频", hr); err != nil {
		return err
	}
	defer coUninitialize.Call()

	activation, cancel := context.WithTimeout(ctx, 8*time.Second)
	client, err := openMeetingAudio(activation, pid)
	cancel()
	if client != 0 {
		defer comMethod(client, 2)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	format := waveFormatEx{FormatTag: 1, Channels: 2, SamplesPerSec: 44100, AvgBytesPerSec: 176400, BlockAlign: 4, BitsPerSample: 16}
	const flags = 0x00020000 | 0x00040000 | 0x80000000 // LOOPBACK | EVENTCALLBACK | AUTOCONVERTPCM
	var initResult uintptr
	if unsafe.Sizeof(uintptr(0)) == 4 {
		// REFERENCE_TIME parameters remain 64-bit on 32-bit Windows.
		initResult = comMethod(client, 3, 0, flags, 0, 0, 0, 0, uintptr(unsafe.Pointer(&format)), 0)
	} else {
		initResult = comMethod(client, 3, 0, flags, 0, 0, uintptr(unsafe.Pointer(&format)), 0)
	}
	err = meetingCOMError("初始化应用音频流", initResult)
	if err != nil {
		return err
	}

	var capture uintptr
	if err := meetingCOMError("获取采集器", comMethod(client, 14, uintptr(unsafe.Pointer(&meetingCaptureInterface)), uintptr(unsafe.Pointer(&capture)))); err != nil {
		return err
	}
	if capture == 0 {
		return errors.New("Windows 未返回音频采集器")
	}
	defer comMethod(capture, 2)
	event, _, eventErr := createMeetingEvent.Call(0, 0, 0, 0)
	if event == 0 {
		return fmt.Errorf("创建音频事件: %w", eventErr)
	}
	defer procCloseHandle.Call(event)
	if err := meetingCOMError("绑定音频事件", comMethod(client, 13, event)); err != nil {
		return err
	}
	if err := meetingCOMError("启动音频采集", comMethod(client, 10)); err != nil {
		return err
	}
	defer comMethod(client, 11)
	started()

	converter, err := audio.NewPCMConverter(audio.PCMFormat{
		SampleRate:    int(format.SamplesPerSec),
		Channels:      int(format.Channels),
		BitsPerSample: int(format.BitsPerSample),
	})
	if err != nil {
		return err
	}
	lastPacket := time.Now()
	for ctx.Err() == nil {
		if active, _, _ := waitMeetingEvent.Call(target, 0); active != 258 {
			return errors.New("选中的音频来源已结束，请重新选择")
		}
		wait, _, waitErr := waitMeetingEvent.Call(event, 40)
		if err := ctx.Err(); err != nil {
			return err
		}
		switch wait {
		case 0:
			for ctx.Err() == nil {
				n, err := readMeetingPacket(capture, converter, push)
				if err != nil {
					return err
				}
				if n == 0 {
					break
				}
				lastPacket = time.Now()
			}
		case 258: // A render stream may stop producing packets during silence.
			if time.Since(lastPacket) >= 40*time.Millisecond {
				if err := converter.Consume(nil, int(format.SamplesPerSec)/25, true, push); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("等待应用音频: %w", waitErr)
		}
	}
	return ctx.Err()
}

func readMeetingPacket(capture uintptr, converter *audio.PCMConverter, push func([]byte)) (uint32, error) {
	var data uintptr
	var frames, flags uint32
	hr := comMethod(capture, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0)
	if err := meetingCOMError("读取应用音频", hr); err != nil || frames == 0 {
		return frames, err
	}

	// Each successful nonempty GetBuffer owns a matching ReleaseBuffer,
	// including malformed packets and conversion failures.
	packetBytes, packetErr := converter.PacketBytes(int(frames))
	var pcm []byte
	if packetErr == nil && flags&2 == 0 {
		if data == 0 {
			packetErr = errors.New("Windows 返回了空音频缓冲区")
		} else {
			pcm = unsafe.Slice((*byte)(unsafe.Pointer(data)), packetBytes)
		}
	}
	if packetErr == nil {
		packetErr = converter.Consume(pcm, int(frames), flags&2 != 0, push)
	}
	releaseErr := meetingCOMError("释放音频缓冲", comMethod(capture, 4, uintptr(frames)))
	return frames, errors.Join(packetErr, releaseErr)
}
