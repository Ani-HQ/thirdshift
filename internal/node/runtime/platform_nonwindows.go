//go:build !windows

package runtime

import "os/exec"

type devPlatformControls struct{}

func newPlatformControls(LaunchConfig) platformControls {
	return devPlatformControls{}
}

func (devPlatformControls) BeforeStart(*exec.Cmd) error {
	return nil
}

func (devPlatformControls) AfterStart(int) error {
	return nil
}

func (devPlatformControls) Cleanup() error {
	return nil
}
