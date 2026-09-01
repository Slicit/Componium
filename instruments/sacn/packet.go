// Package sacn drives DMX lighting over the network using E1.31 (streaming
// ACN), the standard every modern lighting node and most LED controllers
// already speak.
//
// This is the point of ADR 0001: Componium does not invent a lighting
// protocol, it speaks the one the ecosystem uses, and inherits every fixture
// ever built for it.
package sacn

import (
	"encoding/binary"
	"fmt"
)

// Slots is the number of DMX channels in a universe.
const Slots = 512

// packetLen is the full E1.31 datagram: 38 bytes of root layer, 77 of framing
// layer, 523 of DMP layer.
const packetLen = 38 + 77 + 523

var acnID = [12]byte{0x41, 0x53, 0x43, 0x2d, 0x45, 0x31, 0x2e, 0x31, 0x37, 0x00, 0x00, 0x00}

// Packet builds one E1.31 datagram carrying a full universe.
//
// Layout is E1.31-2018 section 4.1. The three nested PDUs each carry their own
// flags-and-length word, where the top nibble is 0x7 and the remaining twelve
// bits are the length from that word to the end of the datagram.
type Packet struct {
	CID        [16]byte
	SourceName string
	Universe   uint16
	Priority   uint8
	Sequence   uint8
	Data       [Slots]byte
}

// Marshal renders the packet onto the wire.
func (p *Packet) Marshal() []byte {
	b := make([]byte, packetLen)

	// --- root layer ---
	binary.BigEndian.PutUint16(b[0:], 0x0010) // preamble size
	binary.BigEndian.PutUint16(b[2:], 0x0000) // postamble size
	copy(b[4:], acnID[:])
	binary.BigEndian.PutUint16(b[16:], flagsAndLength(packetLen-16))
	binary.BigEndian.PutUint32(b[18:], 0x00000004) // VECTOR_ROOT_E131_DATA
	copy(b[22:], p.CID[:])

	// --- framing layer ---
	binary.BigEndian.PutUint16(b[38:], flagsAndLength(packetLen-38))
	binary.BigEndian.PutUint32(b[40:], 0x00000002) // VECTOR_E131_DATA_PACKET
	name := p.SourceName
	if len(name) > 63 {
		name = name[:63]
	}
	copy(b[44:], name)
	prio := p.Priority
	if prio == 0 {
		prio = 100
	}
	b[108] = prio
	binary.BigEndian.PutUint16(b[109:], 0) // synchronisation address, unused
	b[111] = p.Sequence
	b[112] = 0 // options
	binary.BigEndian.PutUint16(b[113:], p.Universe)

	// --- DMP layer ---
	binary.BigEndian.PutUint16(b[115:], flagsAndLength(packetLen-115))
	b[117] = 0x02 // VECTOR_DMP_SET_PROPERTY
	b[118] = 0xa1 // address and data type
	binary.BigEndian.PutUint16(b[119:], 0x0000)
	binary.BigEndian.PutUint16(b[121:], 0x0001)
	binary.BigEndian.PutUint16(b[123:], Slots+1) // start code plus slots
	b[125] = 0x00                                // DMX start code
	copy(b[126:], p.Data[:])

	return b
}

func flagsAndLength(n int) uint16 { return uint16(0x7000 | (n & 0x0fff)) }

// Parse reads a datagram back, for tests and for anything that wants to listen
// to a universe.
func Parse(b []byte) (*Packet, error) {
	if len(b) != packetLen {
		return nil, fmt.Errorf("sacn: got %d bytes, want %d", len(b), packetLen)
	}
	if string(b[4:16]) != string(acnID[:]) {
		return nil, fmt.Errorf("sacn: not an ACN packet")
	}
	if v := binary.BigEndian.Uint32(b[18:]); v != 4 {
		return nil, fmt.Errorf("sacn: root vector %d, want 4", v)
	}
	if v := binary.BigEndian.Uint32(b[40:]); v != 2 {
		return nil, fmt.Errorf("sacn: framing vector %d, want 2", v)
	}
	if b[125] != 0x00 {
		return nil, fmt.Errorf("sacn: start code 0x%02x, want 0x00", b[125])
	}
	p := &Packet{
		Universe: binary.BigEndian.Uint16(b[113:]),
		Priority: b[108],
		Sequence: b[111],
	}
	copy(p.CID[:], b[22:38])
	name := b[44:108]
	if i := indexZero(name); i >= 0 {
		name = name[:i]
	}
	p.SourceName = string(name)
	copy(p.Data[:], b[126:])
	return p, nil
}

func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}

// Port is where E1.31 lives. Fixed by the standard, so a fixture that does not
// listen here is a fixture that is not speaking sACN.
const Port = 5568

// MulticastAddr returns the address a universe is conventionally sent to,
// 239.255.<high>.<low> on port 5568.
func MulticastAddr(universe uint16) string {
	return fmt.Sprintf("239.255.%d.%d:%d", byte(universe>>8), byte(universe), Port)
}
