package invoices

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceTemplateBounds(t *testing.T) {
	valid := &tammyv1.InvoiceTemplateInput{SellerAddress: "27 Cambridge Road", Notes: "Thank you", PaymentInstructions: "Bank transfer", PaymentTermsDays: 365}
	if ValidateTemplate(valid) != nil {
		t.Fatal("valid template rejected")
	}
	for name, value := range map[string]*tammyv1.InvoiceTemplateInput{
		"missing":      nil,
		"terms":        {PaymentTermsDays: 366},
		"address":      {SellerAddress: strings.Repeat("a", 1001)},
		"notes":        {Notes: strings.Repeat("a", 2001)},
		"instructions": {PaymentInstructions: strings.Repeat("a", 1001)},
		"control":      {Notes: "bad\x00"},
		"logo":         {LogoAssetId: ptr("not-an-id")},
		"empty logo":   {LogoAssetId: ptr("")},
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateTemplate(value) == nil {
				t.Fatal("invalid template accepted")
			}
		})
	}
}

func logoBytes(t *testing.T, mime string, width, height int) []byte {
	t.Helper()
	var content bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var err error
	if mime == "image/png" {
		err = png.Encode(&content, img)
	} else {
		err = jpeg.Encode(&content, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return content.Bytes()
}

func TestInvoiceLogoRequiresCompleteBoundedRasterImage(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg"} {
		content := logoBytes(t, mime, 64, 32)
		width, height, err := ValidateLogo(mime, content)
		if err != nil || width != 64 || height != 32 {
			t.Fatalf("%s dimensions=%dx%d error=%v", mime, width, height, err)
		}
		for name, invalid := range map[string][]byte{"truncated": content[:len(content)-1], "trailing": append(append([]byte(nil), content...), 1), "fake": []byte("not an image"), "oversized": make([]byte, 1048577)} {
			t.Run(mime+"/"+name, func(t *testing.T) {
				if _, _, err := ValidateLogo(mime, invalid); err == nil {
					t.Fatal("invalid logo accepted")
				}
			})
		}
		other := "image/png"
		if mime == other {
			other = "image/jpeg"
		}
		if _, _, err := ValidateLogo(other, content); err == nil {
			t.Fatal("MIME mismatch accepted")
		}
	}
	for _, mime := range []string{"image/svg+xml", "application/pdf", ""} {
		if _, _, err := ValidateLogo(mime, []byte("x")); err == nil {
			t.Fatal("unsupported MIME accepted")
		}
	}
	for name, size := range map[string][2]int{"dimension": {4097, 1}, "pixels": {2049, 2049}} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ValidateLogo("image/png", logoBytes(t, "image/png", size[0], size[1])); err == nil {
				t.Fatal("large decoded image accepted")
			}
		})
	}
}

func TestInvoiceLogoEnforcesExactlyFourMillionPixels(t *testing.T) {
	if _, _, err := ValidateLogo("image/png", logoBytes(t, "image/png", 2000, 2000)); err != nil {
		t.Fatalf("four-million-pixel logo rejected: %v", err)
	}
	if _, _, err := ValidateLogo("image/png", logoBytes(t, "image/png", 2001, 2000)); err == nil {
		t.Fatal("logo above four million pixels accepted")
	}
}

func pngLogoChunk(kind string, payload []byte) []byte {
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], kind)
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return chunk
}

func TestInvoiceLogoRejectsAnimationControlChunks(t *testing.T) {
	base := logoBytes(t, "image/png", 64, 32)
	animation := make([]byte, 8)
	binary.BigEndian.PutUint32(animation[:4], 1)
	frame := make([]byte, 26)
	binary.BigEndian.PutUint32(frame[4:8], 64)
	binary.BigEndian.PutUint32(frame[8:12], 32)
	binary.BigEndian.PutUint16(frame[20:22], 1)
	binary.BigEndian.PutUint16(frame[22:24], 1)
	for name, chunks := range map[string][][]byte{
		"complete single-frame APNG": {pngLogoChunk("acTL", animation), pngLogoChunk("fcTL", frame)},
		"animation control":          {pngLogoChunk("acTL", animation)},
		"frame control":              {pngLogoChunk("fcTL", frame)},
		"frame data":                 {pngLogoChunk("fdAT", make([]byte, 4))},
	} {
		t.Run(name, func(t *testing.T) {
			content := append([]byte(nil), base[:33]...)
			for _, chunk := range chunks {
				content = append(content, chunk...)
			}
			content = append(content, base[33:]...)
			if _, _, err := ValidateLogo("image/png", content); err == nil {
				t.Fatal("animated PNG control accepted for static invoice logo")
			}
		})
	}
}
