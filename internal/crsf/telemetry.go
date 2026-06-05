package crsf

// Inbound CRSF telemetry: the ELRS TX module relays frames back over the same
// handset UART we transmit on. We parse the two we care about — LINK_STATISTICS
// (link quality / RSSI, the "robust connection" signal) and BATTERY_SENSOR.
//
// A telemetry frame has the same envelope as the RC frame:
//
//	[addr/sync] [len] [type] [payload...] [crc8]
//
// where len counts type+payload+crc, so the whole frame is 2+len bytes and the
// CRC (DVB-S2, same as outbound) covers [type+payload]. We don't trust the sync
// byte (several addresses are valid); instead we resync on the CRC.

const (
	FrameTypeLinkStats byte = 0x14 // LINK_STATISTICS
	FrameTypeBattery   byte = 0x08 // BATTERY_SENSOR

	// A CRSF frame's len byte counts type+payload+crc; payload is len-2.
	maxFrameLen = 62  // CRSF caps len at 62
	parserCap   = 512 // bound the resync buffer against garbage
)

// LinkStats is the decoded LINK_STATISTICS payload. RSSI values are reported as
// positive magnitudes on the wire; dBm is the negation.
type LinkStats struct {
	UplinkRSSI1  uint8
	UplinkRSSI2  uint8
	UplinkLQ     uint8 // link quality, 0-100 %
	UplinkSNR    int8
	ActiveAnt    uint8
	RFMode       uint8
	TXPowerIdx   uint8
	DownlinkRSSI uint8
	DownlinkLQ   uint8
	DownlinkSNR  int8
}

// Battery is the decoded BATTERY_SENSOR payload.
type Battery struct {
	Voltage   float64 // volts
	Current   float64 // amps
	Capacity  int     // mAh used
	Remaining uint8   // % remaining
}

// Parser accumulates serial bytes and emits complete, CRC-valid CRSF frames. It is
// not safe for concurrent use (one reader goroutine owns it).
type Parser struct {
	buf []byte
}

// Push appends data and calls onFrame for each complete, CRC-valid frame found,
// passing the frame type and its payload (a slice valid only for the duration of
// the call). Invalid candidates are resynced past one byte at a time.
func (p *Parser) Push(data []byte, onFrame func(typ byte, payload []byte)) {
	p.buf = append(p.buf, data...)
	for {
		if len(p.buf) < 2 {
			return
		}
		length := int(p.buf[1])
		if length < 2 || length > maxFrameLen {
			p.drop1()
			continue
		}
		total := length + 2 // addr + len + (type+payload+crc)
		if len(p.buf) < total {
			// Incomplete; wait for more — unless the buffer is implausibly large,
			// which means we're stuck on garbage that merely looks like a header.
			if len(p.buf) > parserCap {
				p.drop1()
				continue
			}
			return
		}
		body := p.buf[2 : total-1] // [type + payload]
		if CRC8(body) != p.buf[total-1] {
			p.drop1()
			continue
		}
		onFrame(body[0], body[1:])
		p.buf = p.buf[total:]
	}
}

func (p *Parser) drop1() {
	p.buf = p.buf[1:]
}

// DecodeLinkStats parses a LINK_STATISTICS payload (10 bytes).
func DecodeLinkStats(payload []byte) (LinkStats, bool) {
	if len(payload) < 10 {
		return LinkStats{}, false
	}
	return LinkStats{
		UplinkRSSI1:  payload[0],
		UplinkRSSI2:  payload[1],
		UplinkLQ:     payload[2],
		UplinkSNR:    int8(payload[3]),
		ActiveAnt:    payload[4],
		RFMode:       payload[5],
		TXPowerIdx:   payload[6],
		DownlinkRSSI: payload[7],
		DownlinkLQ:   payload[8],
		DownlinkSNR:  int8(payload[9]),
	}, true
}

// DecodeBattery parses a BATTERY_SENSOR payload (8 bytes): voltage (dV, big-endian),
// current (dA, big-endian), capacity used (mAh, 24-bit big-endian), % remaining.
func DecodeBattery(payload []byte) (Battery, bool) {
	if len(payload) < 8 {
		return Battery{}, false
	}
	volt := uint16(payload[0])<<8 | uint16(payload[1])
	curr := uint16(payload[2])<<8 | uint16(payload[3])
	cap := int(payload[4])<<16 | int(payload[5])<<8 | int(payload[6])
	return Battery{
		Voltage:   float64(volt) / 10,
		Current:   float64(curr) / 10,
		Capacity:  cap,
		Remaining: payload[7],
	}, true
}

// txPowerTable maps the CRSF TX-power enum index to milliwatts.
var txPowerTable = [...]int{0, 10, 25, 100, 500, 1000, 2000, 250, 50}

// TXPowerMW converts a LINK_STATISTICS TX-power index to milliwatts (0 if unknown).
func TXPowerMW(idx uint8) int {
	if int(idx) < len(txPowerTable) {
		return txPowerTable[idx]
	}
	return 0
}
