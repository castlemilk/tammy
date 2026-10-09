package audit

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPurchaseAuditPayloadHasClosedSpecificActions(t *testing.T) {
	file := tammyv1.File_tammy_v1_events_proto
	message := file.Messages().ByName("PurchaseAccountingChangedEvent")
	if message == nil {
		t.Fatal("purchase mutations have no specific ordinary audit payload")
	}
	actions := file.Enums().ByName("PurchaseAuditAction")
	if actions == nil || actions.Values().Len() != 7 {
		t.Fatal("purchase audit requires exactly six accepted mutation actions and unspecified")
	}
	for _, name := range []string{"PURCHASE_AUDIT_ACTION_POSTED", "PURCHASE_AUDIT_ACTION_PAYMENT_RECORDED", "PURCHASE_AUDIT_ACTION_TAX_EVIDENCE_RECORDED", "PURCHASE_AUDIT_ACTION_GST_ATTRIBUTED", "PURCHASE_AUDIT_ACTION_GST_DEFERRED", "PURCHASE_AUDIT_ACTION_LEGACY_REVIEW_RESOLVED"} {
		if actions.Values().ByName(protoreflect.Name(name)) == nil {
			t.Fatalf("missing specific purchase action %s", name)
		}
	}
	if message.Fields().Len() != 3 || message.Fields().ByName("action") == nil || message.Fields().ByName("accounting_version") == nil || message.Fields().ByName("financial_revision") == nil {
		t.Fatal("purchase audit payload must contain only closed action and committed revisions")
	}
	union := file.Messages().ByName("AuditEventPayload")
	if union.Fields().ByName("purchase_accounting_changed") == nil {
		t.Fatal("purchase audit body is absent from closed ordinary payload union")
	}
}
