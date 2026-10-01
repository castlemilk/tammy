package contracts_test

import (
	"testing"

	_ "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestInvoiceBrandingContractsPreserveSnapshotsAndBoundLogoInput(t *testing.T) {
	for _, name := range []protoreflect.FullName{"tammy.v1.InvoiceTemplateInput", "tammy.v1.InvoiceTemplate", "tammy.v1.InvoiceLogo", "tammy.v1.UploadInvoiceLogoRequest", "tammy.v1.GetInvoiceLogoResponse"} {
		if _, err := protoregistry.GlobalFiles.FindDescriptorByName(name); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	for name, number := range map[protoreflect.FullName]protoreflect.FieldNumber{"tammy.v1.Invoice": 15, "tammy.v1.SaveInvoiceDraftRequest": 6} {
		d, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
		if err != nil {
			t.Fatal(err)
		}
		field := d.(protoreflect.MessageDescriptor).Fields().ByName("logo_asset_id")
		if field == nil || field.Number() != number || !field.HasPresence() {
			t.Errorf("%s needs optional logo_asset_id field %d", name, number)
		}
	}
	d, err := protoregistry.GlobalFiles.FindDescriptorByName("tammy.v1.InvoiceService")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []protoreflect.Name{"GetInvoiceTemplate", "SaveInvoiceTemplate", "UploadInvoiceLogo", "GetInvoiceLogo"} {
		if d.(protoreflect.ServiceDescriptor).Methods().ByName(name) == nil {
			t.Errorf("missing invoice RPC %s", name)
		}
	}
}
