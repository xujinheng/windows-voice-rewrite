//go:build windows

package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestASRChildHasNoConsole(t *testing.T) {
	cmd := exec.Command("llama-server.exe")
	asrChildHost.Prepare(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("ASR child would create a console window")
	}
}

func TestWindowsJobClosesChildOnHostExit(t *testing.T) {
	if os.Getenv("VOICEREWRITE_ASR_JOB_CHILD") == "1" {
		time.Sleep(20 * time.Second)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsJobClosesChildOnHostExit$")
	cmd.Env = append(os.Environ(), "VOICEREWRITE_ASR_JOB_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	release, err := protectChild(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	release() // OS closes the job on abrupt host exit too.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing job did not terminate the ASR child")
	}
}
