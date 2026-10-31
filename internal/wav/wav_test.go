package wav

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestDecodeRoundTrip(t *testing.T) {
	in := []int16{0, 1, -1, 32767, -32768, 1234}
	got, rate, err := Decode(Encode(in, 24000))
	if err != nil || rate != 24000 || !reflect.DeepEqual(got, in) {
		t.Fatalf("Decode(Encode) = %v, %d, %v", got, rate, err)
	}
}

func TestDecodeSkipsOtherChunks(t *testing.T) {
	enc := Encode([]int16{7, 8}, 22050)
	// Insert an odd-sized LIST chunk (padded to even) between fmt and data.
	var b bytes.Buffer
	b.Write(enc[:36])
	b.WriteString("LIST")
	binary.Write(&b, binary.LittleEndian, uint32(3))
	b.Write([]byte{1, 2, 3, 0})
	b.Write(enc[36:])
	got, _, err := Decode(b.Bytes())
	if err != nil || !reflect.DeepEqual(got, []int16{7, 8}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestDecodeRejectsStereo(t *testing.T) {
	enc := Encode([]int16{1, 2}, 22050)
	binary.LittleEndian.PutUint16(enc[22:], 2)
	if _, _, err := Decode(enc); err == nil {
		t.Fatal("stereo must be rejected")
	}
}
