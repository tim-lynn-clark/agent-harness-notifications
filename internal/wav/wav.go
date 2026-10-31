// Package wav reads and writes 16-bit mono PCM WAV, the only format cnw
// generates.
package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Encode wraps mono 16-bit samples at rate in a RIFF/WAVE container.
func Encode(samples []int16, rate int) []byte {
	dataLen := uint32(2 * len(samples))
	var b bytes.Buffer
	b.Grow(44 + int(dataLen))
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, 36+dataLen)
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))     // fmt chunk size
	binary.Write(&b, binary.LittleEndian, uint16(1))      // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))      // mono
	binary.Write(&b, binary.LittleEndian, uint32(rate))   // sample rate
	binary.Write(&b, binary.LittleEndian, uint32(rate*2)) // byte rate
	binary.Write(&b, binary.LittleEndian, uint16(2))      // block align
	binary.Write(&b, binary.LittleEndian, uint16(16))     // bits per sample
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, dataLen)
	binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

// Decode reads a 16-bit mono PCM WAV file's samples and sample rate. Chunks
// other than "fmt " and "data" are skipped.
func Decode(data []byte) (samples []int16, rate int, err error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a RIFF/WAVE file")
	}
	le := binary.LittleEndian
	var haveFmt bool
	for p := 12; p+8 <= len(data); {
		id, size := string(data[p:p+4]), int(le.Uint32(data[p+4:]))
		body := data[p+8:]
		if size > len(body) {
			return nil, 0, fmt.Errorf("chunk %q overruns the file", id)
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, errors.New("short fmt chunk")
			}
			format, channels, bits := le.Uint16(body[0:]), le.Uint16(body[2:]), le.Uint16(body[14:])
			if format != 1 || channels != 1 || bits != 16 {
				return nil, 0, fmt.Errorf("want 16-bit mono PCM, got format %d, %d channels, %d bits", format, channels, bits)
			}
			rate = int(le.Uint32(body[4:]))
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, 0, errors.New("data chunk before fmt chunk")
			}
			samples = make([]int16, size/2)
			for i := range samples {
				samples[i] = int16(le.Uint16(body[2*i:]))
			}
			return samples, rate, nil
		}
		p += 8 + size + size%2 // chunks are padded to an even length
	}
	return nil, 0, errors.New("no data chunk")
}
