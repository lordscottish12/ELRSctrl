// Package crsf implements the parts of the TBS Crossfire / ExpressLRS (ELRS)
// serial protocol needed to drive a TX module as if we were an EdgeTX handset:
// the RC_CHANNELS_PACKED frame.
//
// Frame layout (8N1; handset-UART baud is set by config — 115200 over the Aeris
// Link USB, not the 420000 over-air rate):
//
//	[addr] [len=0x18] [type=0x16] [22 bytes payload] [crc8]
//
//   - addr     destination device address; 0xEE = CRSF transmitter (handset->module)
//   - len      0x18 (24) = type(1) + payload(22) + crc(1)
//   - type     0x16 = RC_CHANNELS_PACKED
//   - payload  16 channels x 11 bits, LSB-first / little-endian, no padding
//   - crc8     DVB-S2, polynomial 0xD5, over [type + payload] only
//
// Channel tick values map to servo pulse widths:
//
//	172 -> ~1000us, 992 -> 1500us (center), 1811 -> ~2000us
package crsf

const (
	// Device addresses (a.k.a. the sync byte / first frame byte).
	AddrTransmitter      byte = 0xEE // CRSF_ADDRESS_CRSF_TRANSMITTER (handset -> module)
	AddrFlightController byte = 0xC8 // CRSF_ADDRESS_FLIGHT_CONTROLLER (also widely accepted)

	FrameTypeRCChannels byte = 0x16 // RC_CHANNELS_PACKED
	FrameLengthRC       byte = 0x18 // type + 22 payload + crc

	// FrameSizeRC is the total wire size: addr + len + type + 22 + crc.
	FrameSizeRC = 26
	// NumChannels packed into one RC frame.
	NumChannels = 16

	// Standard CRSF channel tick values.
	TicksMin   uint16 = 172  // ~1000us
	TicksMid   uint16 = 992  // 1500us (center)
	TicksMax   uint16 = 1811 // ~2000us
	TicksFloor uint16 = 0    // hard lower bound (11-bit)
	TicksCeil  uint16 = 1984 // practical upper bound used by CRSF
)

// crc8Table is the precomputed DVB-S2 (poly 0xD5) lookup table.
var crc8Table [256]byte

func init() {
	const poly = 0xD5
	for i := 0; i < 256; i++ {
		crc := byte(i)
		for b := 0; b < 8; b++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
		crc8Table[i] = crc
	}
}

// CRC8 computes the CRSF DVB-S2 CRC over data (init 0x00, no reflection, no final xor).
func CRC8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc = crc8Table[crc^b]
	}
	return crc
}

// ClampTicks bounds a channel value to the valid 11-bit CRSF range [0, 1984].
func ClampTicks(v int) uint16 {
	if v < int(TicksFloor) {
		return TicksFloor
	}
	if v > int(TicksCeil) {
		return TicksCeil
	}
	return uint16(v)
}

// PackChannels packs 16 channel values (each masked to 11 bits) into 22 bytes,
// LSB-first across byte boundaries — the canonical betaflight/ELRS bit packing.
func PackChannels(ch [NumChannels]uint16) [22]byte {
	var out [22]byte
	bit := 0
	for _, c := range ch {
		v := uint32(c & 0x07FF)
		for b := 0; b < 11; b++ {
			if v&(1<<uint(b)) != 0 {
				out[bit/8] |= 1 << uint(bit%8)
			}
			bit++
		}
	}
	return out
}

// UnpackChannels reverses PackChannels (used by tests and any future RX parsing).
func UnpackChannels(p [22]byte) [NumChannels]uint16 {
	var ch [NumChannels]uint16
	bit := 0
	for i := range ch {
		var v uint32
		for b := 0; b < 11; b++ {
			if p[bit/8]&(1<<uint(bit%8)) != 0 {
				v |= 1 << uint(b)
			}
			bit++
		}
		ch[i] = uint16(v)
	}
	return ch
}

// BuildRCFrame assembles a complete RC_CHANNELS_PACKED frame ready for the wire.
func BuildRCFrame(addr byte, ch [NumChannels]uint16) []byte {
	payload := PackChannels(ch)
	frame := make([]byte, 0, FrameSizeRC)
	frame = append(frame, addr, FrameLengthRC, FrameTypeRCChannels)
	frame = append(frame, payload[:]...)
	frame = append(frame, CRC8(frame[2:])) // crc over [type + payload]
	return frame
}

// TicksToMicros converts a CRSF tick value to its servo pulse width in microseconds.
func TicksToMicros(t uint16) int {
	return 1500 + (5*(int(t)-int(TicksMid)))/8
}

// MicrosToTicks converts a servo pulse width in microseconds to a CRSF tick value.
func MicrosToTicks(us int) uint16 {
	return ClampTicks(int(TicksMid) + (8*(us-1500))/5)
}
