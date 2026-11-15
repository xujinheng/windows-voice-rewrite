//go:build windows

package main

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestMeetingCompletionHandlerIsAgile(t *testing.T) {
	self, _, err := procGlobalAlloc.Call(0x0040, unsafe.Sizeof(meetingActivationObject{}))
	if self == 0 {
		t.Fatalf("allocate COM callback: %v", err)
	}
	activation := &meetingActivation{}
	activation.refs.Store(1)
	meetingActivations.Store(self, activation)
	defer meetingRelease(self)
	(*meetingActivationObject)(unsafe.Pointer(self)).vtable = uintptr(unsafe.Pointer(&meetingActivationVTable[0]))

	// Use the SDK's IID independently of the implementation. WASAPI probes
	// for this marker before it accepts the asynchronous completion handler.
	iid := syscall.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	var agile uintptr
	hr := comMethod(self, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&agile)))
	if hr != 0 || agile != self {
		t.Fatalf("QueryInterface(IAgileObject) = 0x%08X, %p; want S_OK, callback identity", uint32(hr), unsafe.Pointer(agile))
	}
	if refs := comMethod(agile, 2); refs != 1 {
		t.Fatalf("Release(IAgileObject) = %d; want 1", refs)
	}
}
