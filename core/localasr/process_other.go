//go:build !windows

package localasr

import "os/exec"

func hideChildConsole(_ *exec.Cmd) {}
