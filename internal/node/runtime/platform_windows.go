//go:build windows

package runtime

import "os/exec"

type windowsPlatformControls struct{}

func newPlatformControls(LaunchConfig) platformControls {
	return windowsPlatformControls{}
}

func (windowsPlatformControls) BeforeStart(*exec.Cmd) error {
	// Windows firewall and restricted-token setup are wired behind this build tag.
	// Real-machine verification fills in the concrete netsh/Job Object policy.
	return nil
}

func (windowsPlatformControls) AfterStart(int) error {
	// Job Object assignment belongs here once validated on Windows NVIDIA hosts.
	return nil
}

func (windowsPlatformControls) Cleanup() error {
	// Firewall rule removal belongs here once the concrete rule name is finalized.
	return nil
}
