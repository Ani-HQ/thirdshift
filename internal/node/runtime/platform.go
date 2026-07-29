package runtime

import "os/exec"

type platformControls interface {
	BeforeStart(cmd *exec.Cmd) error
	AfterStart(pid int) error
	Cleanup() error
}
