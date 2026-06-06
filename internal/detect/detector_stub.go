//go:build !linux

package detect

import "errors"

// NewDetector reports that ONNX person detection isn't available off Linux, so the
// Windows dev build stays cgo-free. The UI treats the error as "detection disabled".
func NewDetector(cfg DetectorConfig) (Detector, error) {
	return nil, errors.New("person detection is only available on Linux")
}
