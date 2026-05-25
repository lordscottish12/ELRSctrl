package crsf

import (
	"math/rand"
	"testing"
)

// refCRC8 is an independent, table-free DVB-S2 (poly 0xD5) implementation used
// to cross-check the package's table-driven CRC8.
func refCRC8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0xD5
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func TestCRC8MatchesReference(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 1000; n++ {
		buf := make([]byte, r.Intn(40))
		for i := range buf {
			buf[i] = byte(r.Intn(256))
		}
		if got, want := CRC8(buf), refCRC8(buf); got != want {
			t.Fatalf("CRC8(%x)=0x%02x, ref=0x%02x", buf, got, want)
		}
	}
}

func TestPackUnpackRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for n := 0; n < 1000; n++ {
		var ch [NumChannels]uint16
		for i := range ch {
			ch[i] = uint16(r.Intn(2048)) // full 11-bit range
		}
		got := UnpackChannels(PackChannels(ch))
		if got != ch {
			t.Fatalf("round-trip mismatch:\n in=%v\nout=%v", ch, got)
		}
	}
}

func TestPackAllCenter(t *testing.T) {
	var ch [NumChannels]uint16
	for i := range ch {
		ch[i] = TicksMid
	}
	un := UnpackChannels(PackChannels(ch))
	for i, v := range un {
		if v != TicksMid {
			t.Fatalf("channel %d = %d, want %d", i, v, TicksMid)
		}
	}
}

func TestBuildRCFrameShape(t *testing.T) {
	var ch [NumChannels]uint16
	for i := range ch {
		ch[i] = TicksMid
	}
	f := BuildRCFrame(AddrTransmitter, ch)

	if len(f) != FrameSizeRC {
		t.Fatalf("frame length = %d, want %d", len(f), FrameSizeRC)
	}
	if f[0] != AddrTransmitter {
		t.Errorf("addr = 0x%02x, want 0x%02x", f[0], AddrTransmitter)
	}
	if f[1] != FrameLengthRC {
		t.Errorf("len = 0x%02x, want 0x%02x", f[1], FrameLengthRC)
	}
	if f[2] != FrameTypeRCChannels {
		t.Errorf("type = 0x%02x, want 0x%02x", f[2], FrameTypeRCChannels)
	}
	// CRC is computed over [type + payload] = f[2:25], stored at f[25].
	if want := CRC8(f[2 : FrameSizeRC-1]); f[FrameSizeRC-1] != want {
		t.Errorf("crc = 0x%02x, want 0x%02x", f[FrameSizeRC-1], want)
	}
	if got := refCRC8(f[2 : FrameSizeRC-1]); f[FrameSizeRC-1] != got {
		t.Errorf("crc disagrees with reference impl: 0x%02x vs 0x%02x", f[FrameSizeRC-1], got)
	}
}

func TestClampTicks(t *testing.T) {
	cases := []struct {
		in   int
		want uint16
	}{
		{-100, TicksFloor},
		{0, TicksFloor},
		{992, 992},
		{1984, TicksCeil},
		{5000, TicksCeil},
	}
	for _, c := range cases {
		if got := ClampTicks(c.in); got != c.want {
			t.Errorf("ClampTicks(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMicrosTicksAroundCenter(t *testing.T) {
	if got := TicksToMicros(TicksMid); got != 1500 {
		t.Errorf("TicksToMicros(center) = %d, want 1500", got)
	}
	if got := MicrosToTicks(1500); got != TicksMid {
		t.Errorf("MicrosToTicks(1500) = %d, want %d", got, TicksMid)
	}
	// Endpoints should land close to the canonical 1000/2000us pulses.
	if us := TicksToMicros(TicksMin); us < 985 || us > 1015 {
		t.Errorf("TicksToMicros(min) = %d, want ~1000", us)
	}
	if us := TicksToMicros(TicksMax); us < 1985 || us > 2015 {
		t.Errorf("TicksToMicros(max) = %d, want ~2000", us)
	}
}
