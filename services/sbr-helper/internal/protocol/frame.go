package protocol

import (
	"encoding/binary"
	"io"
)

const MaxPayloadSize = 1 << 20

// ReadFrame reads one complete frame and requires EOF immediately afterwards.
// The caller owns a successful payload and must overwrite it after decoding.
func ReadFrame(reader io.Reader) ([]byte, error) {
	return readFrame(reader, nil)
}

func readFrame(reader io.Reader, observeCleanup func([]byte)) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, protocolError("FRAME_SHORT")
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxPayloadSize {
		return nil, protocolError("FRAME_SIZE_INVALID")
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(reader, payload); err != nil {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_SHORT")
	}
	var trailing [1]byte
	n, err := reader.Read(trailing[:])
	if n != 0 || err == nil {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_TRAILING")
	}
	if err != io.EOF {
		zeroBytes(payload)
		if observeCleanup != nil {
			observeCleanup(payload)
		}
		return nil, protocolError("FRAME_READ")
	}
	return payload, nil
}

// WriteFrame writes the header and payload completely, including to short writers.
func WriteFrame(writer io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxPayloadSize {
		return protocolError("FRAME_SIZE_INVALID")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeAll(writer, header[:]); err != nil {
		return err
	}
	return writeAll(writer, payload)
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil || n <= 0 || n > len(data) {
			return protocolError("FRAME_WRITE")
		}
		data = data[n:]
	}
	return nil
}
