package localasr

import (
	"os/exec"
	"testing"
)

func TestHideChildConsole(t *testing.T) {
	cmd := exec.Command("llama-server.exe")
	hideChildConsole(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("background ASR process would open a console window")
	}
}
