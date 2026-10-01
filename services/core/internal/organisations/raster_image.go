package organisations

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

// ValidateRasterImage performs the same strict full-image validation for evidence and invoice branding.
// It rejects MIME mismatches, incomplete images, trailing bytes, and oversized decoded images before allocation.
func ValidateRasterImage(mimeType string, content []byte, maxDimension, maxPixels uint64) (uint32, uint32, error) {
	invalid := errors.New("organisations: invalid raster image")
	if maxDimension == 0 || maxPixels == 0 ||
		(mimeType != "image/png" && mimeType != "image/jpeg") ||
		(mimeType == "image/png" && !validPNGStructure(content)) ||
		(mimeType == "image/jpeg" && !validJPEGStructure(content)) {
		return 0, 0, invalid
	}
	configurationReader := newEvidenceCountingReader(content)
	configuration, format, err := image.DecodeConfig(configurationReader)
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
		uint64(configuration.Width) > maxDimension || uint64(configuration.Height) > maxDimension ||
		uint64(configuration.Width)*uint64(configuration.Height) > maxPixels {
		return 0, 0, invalid
	}
	decodedReader := newEvidenceCountingReader(content)
	decoded, decodedFormat, err := image.Decode(decodedReader)
	expectedFormat := "png"
	if mimeType == "image/jpeg" {
		expectedFormat = "jpeg"
	}
	if err != nil || format != expectedFormat || decodedFormat != expectedFormat ||
		decoded.Bounds().Dx() != configuration.Width || decoded.Bounds().Dy() != configuration.Height ||
		decodedReader.count != len(content) {
		return 0, 0, invalid
	}
	return uint32(configuration.Width), uint32(configuration.Height), nil
}

type evidenceCountingReader struct {
	reader *bytes.Reader
	count  int
}

func newEvidenceCountingReader(content []byte) *evidenceCountingReader {
	return &evidenceCountingReader{reader: bytes.NewReader(content)}
}

func (reader *evidenceCountingReader) Read(destination []byte) (int, error) {
	count, err := reader.reader.Read(destination)
	reader.count += count
	return count, err
}

func validPNGStructure(content []byte) bool {
	signature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(content, signature) {
		return false
	}
	position := len(signature)
	seenHeader := false
	seenImageData := false
	for position < len(content) {
		if len(content)-position < 12 {
			return false
		}
		length := uint64(binary.BigEndian.Uint32(content[position : position+4]))
		if length > uint64(len(content)-position-12) {
			return false
		}
		typeStart := position + 4
		dataEnd := typeStart + 4 + int(length)
		chunkEnd := dataEnd + 4
		chunkType := string(content[typeStart : typeStart+4])
		if crc32.ChecksumIEEE(content[typeStart:dataEnd]) != binary.BigEndian.Uint32(content[dataEnd:chunkEnd]) {
			return false
		}
		position = chunkEnd
		switch chunkType {
		case "IHDR":
			if seenHeader || length != 13 || typeStart != len(signature)+4 {
				return false
			}
			seenHeader = true
		case "IDAT":
			if !seenHeader {
				return false
			}
			seenImageData = true
		case "IEND":
			return seenHeader && seenImageData && length == 0 && position == len(content)
		}
	}
	return false
}

func validJPEGStructure(content []byte) bool {
	if len(content) < 4 || content[0] != 0xff || content[1] != 0xd8 {
		return false
	}
	position := 2
	inScan := false
	for position < len(content) {
		if inScan && content[position] != 0xff {
			position++
			continue
		}
		if content[position] != 0xff {
			return false
		}
		for position < len(content) && content[position] == 0xff {
			position++
		}
		if position >= len(content) {
			return false
		}
		marker := content[position]
		position++
		if inScan {
			switch {
			case marker == 0x00:
				continue
			case marker >= 0xd0 && marker <= 0xd7:
				continue
			case marker == 0xd9:
				return position == len(content)
			}
			inScan = false
		} else if marker == 0x00 {
			return false
		}
		if marker == 0xd9 {
			return position == len(content)
		}
		if marker == 0xd8 {
			return false
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if len(content)-position < 2 {
			return false
		}
		segmentLength := int(binary.BigEndian.Uint16(content[position : position+2]))
		if segmentLength < 2 || segmentLength > len(content)-position {
			return false
		}
		position += segmentLength
		if marker == 0xda {
			inScan = true
		}
	}
	return false
}
