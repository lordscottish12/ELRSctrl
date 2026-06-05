//go:build !linux

package video

// This stub lets non-Linux platforms (the Windows dev box) build and run the UI
// without V4L2. It never produces frames; the Run screen shows the Err message.

// Capture is the no-op capture handle on platforms without V4L2.
type Capture struct {
	buf *Buffer
}

// ListDevices reports no capture devices off Linux.
func ListDevices() []string { return nil }

// Open returns a capture that never yields a frame and reports why via Err. It
// deliberately returns no error so the UI treats it like a live-but-empty feed
// and renders the placeholder rather than a failure.
func Open(dev string) (*Capture, error) {
	return &Capture{buf: &Buffer{}}, nil
}

func (c *Capture) Buffer() *Buffer { return c.buf }
func (c *Capture) Err() string     { return "video capture not supported on this platform" }
func (c *Capture) Info() string    { return "" }
func (c *Capture) Close() error    { return nil }
