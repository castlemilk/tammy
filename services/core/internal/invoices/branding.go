package invoices

import (
	"encoding/binary"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const MaxLogoBytes = 1048576

func ValidateTemplate(value *tammyv1.InvoiceTemplateInput) error {
	if value == nil || !text(value.SellerAddress, 1000, false) || !text(value.Notes, 2000, false) ||
		!text(value.PaymentInstructions, 1000, false) || value.PaymentTermsDays > 365 ||
		(value.LogoAssetId != nil && !ids.IsCanonicalV7(*value.LogoAssetId)) {
		return ErrInvalidDraft
	}
	return nil
}

func ValidateLogo(mimeType string, content []byte) (uint32, uint32, error) {
	if len(content) == 0 || len(content) > MaxLogoBytes {
		return 0, 0, ErrInvalidDraft
	}
	width, height, err := organisations.ValidateRasterImage(mimeType, content, 4096, 4000000)
	if err != nil {
		return 0, 0, ErrInvalidDraft
	}
	if mimeType == "image/png" {
		// The full raster validation above proves every chunk's length and CRC.
		// Invoice logos must be static, matching the browser and PDF policy;
		// Go's PNG decoder otherwise silently renders an APNG's first frame.
		for position := 8; position+12 <= len(content); {
			switch string(content[position+4 : position+8]) {
			case "acTL", "fcTL", "fdAT":
				return 0, 0, ErrInvalidDraft
			}
			position += int(binary.BigEndian.Uint32(content[position:position+4])) + 12
		}
	}
	return width, height, nil
}
