package contracts

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestBASQueriesExposeExactEffectiveOriginalAndSeparateLiveCurrentness(t *testing.T) {
	prep := (&tammyv1.GetBasPreparationResponse{}).ProtoReflect().Descriptor()
	if field := prep.Fields().ByName("effective_original"); field == nil || field.Number() != 8 || string(field.Message().FullName()) != "tammy.v1.SourceRef" {
		t.Fatal("preparation query omits the exact effective original needed for revision commands")
	}
	for _, descriptor := range []interface{ ProtoReflect() protoreflect.Message }{&tammyv1.GetBasPreparationResponse{}, &tammyv1.GetFrozenBasResponse{}} {
		field := descriptor.ProtoReflect().Descriptor().Fields().ByName("live_currentness")
		if field == nil || field.Enum() == nil || string(field.Enum().FullName()) != "tammy.v1.BasLiveCurrentness" {
			t.Fatal("query conflates retained frozen identity with live source currentness")
		}
	}
}
