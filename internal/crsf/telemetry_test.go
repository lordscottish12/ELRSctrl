package crsf

import "testing"

// frame wraps a payload in the CRSF envelope with a valid CRC, for feeding Parser.
func frame(addr, typ byte, payload []byte) []byte {
	f := []byte{addr, byte(len(payload) + 2), typ}
	f = append(f, payload...)
	return append(f, CRC8(f[2:]))
}

func collect(t *testing.T, chunks ...[]byte) []struct {
	typ byte
	pay []byte
} {
	t.Helper()
	var p Parser
	var got []struct {
		typ byte
		pay []byte
	}
	for _, c := range chunks {
		p.Push(c, func(typ byte, payload []byte) {
			got = append(got, struct {
				typ byte
				pay []byte
			}{typ, append([]byte(nil), payload...)})
		})
	}
	return got
}

func TestParserDecodesLinkAndBattery(t *testing.T) {
	link := []byte{30, 0, 92, 8, 1, 6, 3, 40, 100, 5} // LQ=92, TXpwr idx 3 (100mW)
	batt := []byte{0x00, 0x7C, 0x00, 0x52, 0x00, 0x01, 0x00, 80} // 12.4V, 8.2A
	got := collect(t, frame(0xEA, FrameTypeLinkStats, link), frame(0xC8, FrameTypeBattery, batt))

	if len(got) != 2 {
		t.Fatalf("got %d frames, want 2", len(got))
	}
	ls, ok := DecodeLinkStats(got[0].pay)
	if !ok || got[0].typ != FrameTypeLinkStats {
		t.Fatalf("link stats decode failed: ok=%v typ=%#x", ok, got[0].typ)
	}
	if ls.UplinkLQ != 92 {
		t.Errorf("UplinkLQ = %d, want 92", ls.UplinkLQ)
	}
	if mw := TXPowerMW(ls.TXPowerIdx); mw != 100 {
		t.Errorf("TX power = %d mW, want 100", mw)
	}

	b, ok := DecodeBattery(got[1].pay)
	if !ok || got[1].typ != FrameTypeBattery {
		t.Fatalf("battery decode failed: ok=%v typ=%#x", ok, got[1].typ)
	}
	if b.Voltage != 12.4 || b.Current != 8.2 {
		t.Errorf("battery = %.1fV %.1fA, want 12.4V 8.2A", b.Voltage, b.Current)
	}
	if b.Remaining != 80 {
		t.Errorf("remaining = %d%%, want 80%%", b.Remaining)
	}
}

func TestParserResyncsPastGarbage(t *testing.T) {
	link := []byte{30, 0, 75, 8, 1, 6, 2, 40, 100, 5}
	// Leading junk that includes a bogus length byte, then a real frame.
	junk := []byte{0xFF, 0x55, 0x03, 0x00}
	got := collect(t, append(junk, frame(0xEA, FrameTypeLinkStats, link)...))
	if len(got) != 1 || got[0].typ != FrameTypeLinkStats {
		t.Fatalf("expected 1 link frame after junk, got %+v", got)
	}
	if ls, _ := DecodeLinkStats(got[0].pay); ls.UplinkLQ != 75 {
		t.Errorf("LQ = %d, want 75", ls.UplinkLQ)
	}
}

func TestParserHandlesSplitFrame(t *testing.T) {
	link := []byte{30, 0, 60, 8, 1, 6, 1, 40, 100, 5}
	full := frame(0xEA, FrameTypeLinkStats, link)
	got := collect(t, full[:5], full[5:]) // split mid-frame across two Pushes
	if len(got) != 1 {
		t.Fatalf("expected 1 frame from a split push, got %d", len(got))
	}
	if ls, _ := DecodeLinkStats(got[0].pay); ls.UplinkLQ != 60 {
		t.Errorf("LQ = %d, want 60", ls.UplinkLQ)
	}
}
