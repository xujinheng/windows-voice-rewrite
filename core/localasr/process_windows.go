package localasr

import (
	"os/exec"
	"syscall"
)

// Child consoles are not part of the GUI; stdout/stderr are handled by Core.
func hideChildConsole(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000 // Windows CREATE_NO_WINDOW
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
