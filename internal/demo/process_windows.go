package demo

import "os/exec"

func configureCollectorProcess(*exec.Cmd) {}

func terminateCollectorProcess(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
