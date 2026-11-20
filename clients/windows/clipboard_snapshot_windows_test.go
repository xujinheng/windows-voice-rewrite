//go:build windows

package main

import (
	"testing"

	"github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/clipboard"
)

func TestClipboardGDIHandlesNotCopiedAsHGlobal(t *testing.T) {
	for _, id := range []uint32{2, 3, 9, 14, 0x80, 0x82, 0x83, 0x8e} {
		if !clipboard.UnsafeFormat(id) {
			t.Errorf("format %#x requires special Windows ownership handling", id)
		}
	}
	for _, id := range []uint32{1, 7, 8, 13, 15, 0xc000} {
		if clipboard.UnsafeFormat(id) {
			t.Errorf("format %#x normally uses HGLOBAL", id)
		}
	}
}
