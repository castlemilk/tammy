package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type shortWriter struct {
	buf bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

func TestProtocolFrameRoundTripAndShortWrites(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	w := &shortWriter{max: 2}
	if err := WriteFrame(w, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(bytes.NewReader(w.buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %x, want %x", got, payload)
	}
	payload[0] = 99
	if got[0] != 1 {
		t.Fatal("frame result aliases input")
	}
}

func TestProtocolFrameRejectsInvalidBoundsAndTrailingData(t *testing.T) {
	oversizeHeader := make([]byte, 4)
	binary.BigEndian.PutUint32(oversizeHeader, MaxPayloadSize+1)
	tests := []struct {
		name string
		data []byte
		code string
	}{
		{name: "empty", data: []byte{0, 0, 0, 0}, code: "FRAME_SIZE_INVALID"},
		{name: "oversize", data: oversizeHeader, code: "FRAME_SIZE_INVALID"},
		{name: "short header", data: []byte{0, 0}, code: "FRAME_SHORT"},
		{name: "short payload", data: []byte{0, 0, 0, 2, 1}, code: "FRAME_SHORT"},
		{name: "trailing", data: []byte{0, 0, 0, 1, 1, 2}, code: "FRAME_TRAILING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadFrame(bytes.NewReader(tt.data))
			assertProtocolError(t, err, tt.code)
		})
	}

	var sink bytes.Buffer
	assertProtocolError(t, WriteFrame(&sink, nil), "FRAME_SIZE_INVALID")
	assertProtocolError(t, WriteFrame(&sink, make([]byte, MaxPayloadSize+1)), "FRAME_SIZE_INVALID")
}

func TestProtocolFrameDoesNotAllocateDeclaredOversize(t *testing.T) {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, MaxPayloadSize+1)
	r := io.MultiReader(bytes.NewReader(header), panicReader{})
	_, err := ReadFrame(r)
	assertProtocolError(t, err, "FRAME_SIZE_INVALID")
}

func TestProtocolFrameClearsAllocatedPayloadOnError(t *testing.T) {
	for name, data := range map[string][]byte{
		"short":    {0, 0, 0, 4, 0xaa, 0xbb},
		"trailing": {0, 0, 0, 2, 0xaa, 0xbb, 0xcc},
	} {
		t.Run(name, func(t *testing.T) {
			observed := false
			_, err := readFrame(bytes.NewReader(data), func(payload []byte) {
				observed = true
				for _, value := range payload {
					if value != 0 {
						t.Fatalf("error payload was not cleared: %x", payload)
					}
				}
			})
			if err == nil || !observed {
				t.Fatalf("readFrame error=%v observed=%v", err, observed)
			}
		})
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 1})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{0, 0, 0, 2, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxPayloadSize+8 {
			t.Skip()
		}
		payload, err := ReadFrame(bytes.NewReader(data))
		if err != nil {
			assertStableFuzzError(t, err)
			return
		}
		zeroBytes(payload)
	})
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("oversize body was read") }

func assertProtocolError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Code() != code || err.Error() != code {
		t.Fatalf("error = %#v, want stable code %s", err, code)
	}
}

func assertStableFuzzError(t *testing.T, err error) {
	t.Helper()
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || protocolErr.Code() == "" || err.Error() != protocolErr.Code() || len(err.Error()) > 64 {
		t.Fatalf("unstable protocol error: %#v", err)
	}
}
