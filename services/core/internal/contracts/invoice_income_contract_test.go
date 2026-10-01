package contracts

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestInvoiceIncomeRPCsAreAvailable(t *testing.T) {
	file, err := protoregistry.GlobalFiles.FindFileByPath("tammy/v1/invoices.proto")
	if err != nil {
		t.Fatal(err)
	}
	service := file.Services().ByName("InvoiceService")
	for _, method := range []string{"GetInvoiceAccounting", "PostInvoiceIncome", "RecordInvoicePayment", "GetBusinessIncomeSummary"} {
		found := false
		for index := 0; index < service.Methods().Len(); index++ {
			found = found || string(service.Methods().Get(index).Name()) == method
		}
		if !found {
			t.Errorf("InvoiceService missing %s", method)
		}
	}
}
