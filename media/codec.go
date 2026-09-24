package media

import (
	"fmt"
	"strings"
)

// Codec is an audio codec we can send and analyse. Only 8 kHz G.711 for now.
type Codec struct {
	Name   string // as in rtpmap: PCMU, PCMA
	PT     uint8  // static payload type
	encode func(int16) byte
	decode *[256]int16
}

var (
	PCMU = Codec{Name: "PCMU", PT: 0, encode: UlawEncode, decode: &ulawTable}
	PCMA = Codec{Name: "PCMA", PT: 8, encode: AlawEncode, decode: &alawTable}
)

const (
	sampleRate      = 8000
	frameSamples    = 160 // 20 ms
	defaultDTMFPT   = 101
	telephoneEvent  = "telephone-event"
	frameDurationMs = 20
)

// CodecByName accepts "PCMU"/"PCMA" and the common aliases "ulaw"/"alaw".
func CodecByName(name string) (Codec, error) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "PCMU", "ULAW", "G711U":
		return PCMU, nil
	case "PCMA", "ALAW", "G711A":
		return PCMA, nil
	}
	return Codec{}, fmt.Errorf("unsupported codec %q (supported: PCMU, PCMA)", name)
}

// ParseCodecs parses a preference list like "PCMA,PCMU".
func ParseCodecs(list string) ([]Codec, error) {
	var out []Codec
	for _, n := range strings.Split(list, ",") {
		if strings.TrimSpace(n) == "" {
			continue
		}
		c, err := CodecByName(n)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty codec list")
	}
	return out, nil
}

func codecByPT(pt uint8) (Codec, bool) {
	switch pt {
	case PCMU.PT:
		return PCMU, true
	case PCMA.PT:
		return PCMA, true
	}
	return Codec{}, false
}
