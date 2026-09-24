package media

// G.711 μ-law and A-law, after the reference implementation by Sun
// Microsystems (public domain) that most VoIP stacks use.

var (
	segUEnd = [8]int{0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF}
	segAEnd = [8]int{0x1F, 0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF}

	ulawTable [256]int16
	alawTable [256]int16
)

func init() {
	for i := range 256 {
		ulawTable[i] = ulawDecode(byte(i))
		alawTable[i] = alawDecode(byte(i))
	}
}

func segment(v int, ends *[8]int) int {
	for i, e := range ends {
		if v <= e {
			return i
		}
	}
	return 8
}

// UlawEncode converts a 16-bit linear sample to μ-law.
func UlawEncode(sample int16) byte {
	const bias, clip = 0x84, 8159
	v := int(sample) >> 2
	mask := 0xFF
	if v < 0 {
		v, mask = -v, 0x7F
	}
	if v > clip {
		v = clip
	}
	v += bias >> 2
	seg := segment(v, &segUEnd)
	if seg >= 8 {
		return byte(0x7F ^ mask) // #nosec G115 -- G.711 keeps values in range by construction
	}
	return byte((seg<<4 | (v>>(seg+1))&0x0F) ^ mask) // #nosec G115 -- G.711 keeps values in range by construction
}

func ulawDecode(u byte) int16 {
	const bias = 0x84
	u = ^u
	t := (int(u&0x0F) << 3) + bias
	t <<= (u & 0x70) >> 4
	if u&0x80 != 0 {
		return int16(bias - t) // #nosec G115 -- G.711 keeps values in range by construction
	}
	return int16(t - bias) // #nosec G115 -- G.711 keeps values in range by construction
}

// AlawEncode converts a 16-bit linear sample to A-law.
func AlawEncode(sample int16) byte {
	v := int(sample) >> 3
	mask := 0xD5
	if v < 0 {
		v, mask = -v-1, 0x55
	}
	seg := segment(v, &segAEnd)
	if seg >= 8 {
		return byte(0x7F ^ mask) // #nosec G115 -- G.711 keeps values in range by construction
	}
	a := seg << 4
	if seg < 2 {
		a |= (v >> 1) & 0x0F
	} else {
		a |= (v >> seg) & 0x0F
	}
	return byte(a ^ mask) // #nosec G115 -- G.711 keeps values in range by construction
}

func alawDecode(a byte) int16 {
	a ^= 0x55
	t := int(a&0x0F) << 4
	switch seg := int(a&0x70) >> 4; seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if a&0x80 != 0 {
		return int16(t) // #nosec G115 -- G.711 keeps values in range by construction
	}
	return int16(-t) // #nosec G115 -- G.711 keeps values in range by construction
}

// UlawDecode and AlawDecode convert one G.711 byte to a linear sample.
func UlawDecode(u byte) int16 { return ulawTable[u] }
func AlawDecode(a byte) int16 { return alawTable[a] }
