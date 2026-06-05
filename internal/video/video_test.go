package video

import "testing"

// A flat gray YUYV macropixel (Y0=Y1=128, U=V=128) decodes to two identical gray
// RGBA pixels. 128 maps to 130 under the BT.601 studio-swing math: this pins both
// the per-pixel split (both pixels share the U/V pair) and the conversion itself.
func TestDecodeYUYVGray(t *testing.T) {
	f := decodeYUYV([]byte{128, 128, 128, 128}, 2, 1)
	if f == nil {
		t.Fatal("decodeYUYV returned nil for a valid 2x1 buffer")
	}
	if f.W != 2 || f.H != 1 || len(f.Pix) != 2*1*4 {
		t.Fatalf("got W=%d H=%d len=%d, want 2x1 with 8 bytes", f.W, f.H, len(f.Pix))
	}
	want := []byte{130, 130, 130, 255, 130, 130, 130, 255}
	for i, b := range want {
		if f.Pix[i] != b {
			t.Fatalf("pixel byte %d = %d, want %d (full Pix=%v)", i, f.Pix[i], b, f.Pix)
		}
	}
}

// Color endpoints exercise the clamps: a max-luma / extreme-chroma macropixel must
// saturate to 255 rather than wrap.
func TestDecodeYUYVClampsHigh(t *testing.T) {
	f := decodeYUYV([]byte{255, 255, 255, 255}, 2, 1)
	if f == nil {
		t.Fatal("decodeYUYV returned nil")
	}
	// R = 298*(255-16)+409*(255-128) >> 8 = far above 255 -> clamps.
	if f.Pix[0] != 255 {
		t.Fatalf("R = %d, want 255 (clamped)", f.Pix[0])
	}
}

// A buffer too short for the claimed dimensions yields nil, not a panic.
func TestDecodeYUYVShortBuffer(t *testing.T) {
	if f := decodeYUYV([]byte{1, 2}, 4, 4); f != nil {
		t.Fatalf("expected nil for short buffer, got %+v", f)
	}
}
