// +build !linux

package bbr

import (
	"os"
)

func enableBBR(*os.File) error {
	return ErrNoSupport
}

func getBandwidthAndRTT(*os.File) (float64, float64, error) {
	return 0.0, 0.0, ErrNoSupport
}
