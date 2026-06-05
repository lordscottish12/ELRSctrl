// Package video captures an analog FPV feed from a UVC capture device (e.g. the
// Skydroid 5.8 GHz OTG receiver, which enumerates as a standard USB Video Class
// camera) and hands the latest decoded frame to the UI.
//
// Like state.Store between the UI and the sender, the capture goroutine and the
// UI are decoupled through a single-slot Buffer: the grabber overwrites the
// latest frame and the UI reads the newest, dropping anything in between. Capture
// must never block — and it lives entirely on the UI/Draw path, never touching
// the CRSF sender goroutine.
//
// V4L2 capture is Linux-only (capture_linux.go); every other platform gets a
// no-op stub (capture_stub.go) so Windows development still builds.
package video

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
)

// Frame is one decoded video frame as tightly-packed RGBA (len == W*H*4), ready
// to hand straight to ebiten.Image.WritePixels.
type Frame struct {
	W, H int
	Pix  []byte
}

// Buffer is a thread-safe single-slot mailbox for the latest frame, mirroring the
// state.Store pattern: the grabber Sets, the UI reads the newest with Latest and
// drops the rest. seq lets the UI skip re-uploading an unchanged frame.
type Buffer struct {
	mu  sync.Mutex
	f   *Frame
	seq uint64
}

// Set publishes a new frame, replacing any frame the UI hasn't consumed yet.
func (b *Buffer) Set(f *Frame) {
	b.mu.Lock()
	b.f = f
	b.seq++
	b.mu.Unlock()
}

// Latest returns the newest frame and its sequence number (nil, 0 before the
// first frame). The UI compares seq to know whether a re-upload is needed.
func (b *Buffer) Latest() (*Frame, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.f, b.seq
}

// decodeJPEG decodes one MJPEG buffer into an RGBA Frame. jpeg.Decode yields a
// *image.YCbCr, so we blit it into a fresh RGBA — which also guarantees the
// returned pixels don't alias any reused capture buffer.
func decodeJPEG(buf []byte) (*Frame, error) {
	img, err := jpeg.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return &Frame{W: b.Dx(), H: b.Dy(), Pix: dst.Pix}, nil
}

// decodeYUYV converts a packed YUYV (a.k.a. YUY2) buffer to a fresh RGBA Frame.
// YUYV stores two pixels per four bytes — Y0 U Y1 V — sharing the chroma pair.
// Returns nil if the buffer is too short for the claimed dimensions.
func decodeYUYV(buf []byte, w, h int) *Frame {
	n := w * h
	if w <= 0 || h <= 0 || len(buf) < n*2 {
		return nil
	}
	pix := make([]byte, n*4)
	j, p := 0, 0
	for i := 0; i < n; i += 2 {
		y0, u, y1, v := int(buf[j]), int(buf[j+1]), int(buf[j+2]), int(buf[j+3])
		j += 4
		r, g, b := yuv2rgb(y0, u, v)
		pix[p], pix[p+1], pix[p+2], pix[p+3] = r, g, b, 0xff
		r, g, b = yuv2rgb(y1, u, v)
		pix[p+4], pix[p+5], pix[p+6], pix[p+7] = r, g, b, 0xff
		p += 8
	}
	return &Frame{W: w, H: h, Pix: pix}
}

// yuv2rgb is the standard BT.601 integer conversion (studio-swing inputs).
func yuv2rgb(y, u, v int) (byte, byte, byte) {
	c := y - 16
	d := u - 128
	e := v - 128
	r := (298*c + 409*e + 128) >> 8
	g := (298*c - 100*d - 208*e + 128) >> 8
	b := (298*c + 516*d + 128) >> 8
	return clip8(r), clip8(g), clip8(b)
}

func clip8(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
