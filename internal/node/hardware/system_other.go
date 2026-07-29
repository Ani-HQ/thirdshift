//go:build !darwin && !linux && !windows

package hardware

import "errors"

func totalMemoryBytes() (uint64, error) {
	return 0, errors.New("memory check is not implemented for this platform")
}

func freeDiskBytes(path string) (uint64, error) {
	return 0, errors.New("disk check is not implemented for this platform")
}
