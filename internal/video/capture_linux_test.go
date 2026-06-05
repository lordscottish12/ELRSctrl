//go:build linux

package video

import (
	"testing"
	"unsafe"
)

// The V4L2 ioctl request numbers encode the size of their struct argument, so the
// Go layouts must match the kernel ABI byte-for-byte. These are the canonical
// sizes on 64-bit Linux; a mismatch would make every ioctl fail (or corrupt
// memory), so pin them here rather than discover it on the Deck.
func TestV4L2StructSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"v4l2_capability", unsafe.Sizeof(v4l2Capability{}), 104},
		{"v4l2_pix_format", unsafe.Sizeof(v4l2PixFormat{}), 48},
		{"v4l2_format", unsafe.Sizeof(v4l2Format{}), 208},
		{"v4l2_requestbuffers", unsafe.Sizeof(v4l2RequestBuffers{}), 20},
		{"v4l2_buffer", unsafe.Sizeof(v4l2Buffer{}), 88},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("sizeof(%s) = %d, want %d", c.name, c.got, c.want)
		}
	}
	// The pix union must sit at offset 8 (after type + 4 pad), matching the kernel.
	if off := unsafe.Offsetof(v4l2Format{}.pix); off != 8 {
		t.Errorf("v4l2_format.pix offset = %d, want 8", off)
	}
}

// Spot-check the FourCC packing against the known V4L2 constants.
func TestFourCC(t *testing.T) {
	if pixFmtYUYV != 0x56595559 { // 'YUYV' little-endian
		t.Errorf("YUYV fourcc = 0x%08x, want 0x56595559", pixFmtYUYV)
	}
	if pixFmtMJPEG != 0x47504a4d { // 'MJPG' little-endian
		t.Errorf("MJPEG fourcc = 0x%08x, want 0x47504a4d", pixFmtMJPEG)
	}
}
