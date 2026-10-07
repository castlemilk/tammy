package documents

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDocumentPageFrameSizeIncludesActualAnyEnvelope(t *testing.T) {
	for _, textSize := range []int{0, 120, 16384, 1048576} {
		response := &tammyv1.ListDocumentsResponse{Documents: []*tammyv1.Document{{ExtractedText: strings.Repeat("x", textSize)}}, Page: &tammyv1.PageInfo{ReturnedCount: 1}}
		cursor := strings.Repeat("c", 271)
		response.Page.NextCursor = &cursor
		encoded, err := anypb.New(response)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := listResponseFrameSize(response), proto.Size(encoded); got != want {
			t.Fatalf("text size %d: frame size=%d want=%d", textSize, got, want)
		}
	}
}

func TestDocumentPagePositionRejectsChangedOrganisationAndNoncanonicalAnchor(t *testing.T) {
	organisationID := "018f0000-0000-7000-8000-000000000020"
	document := &tammyv1.Document{Id: "018f0000-0000-7000-8000-000000000030", CreatedAt: timestamppb.New(time.Date(2026, 10, 1, 0, 0, 0, 120, time.UTC))}
	position := encodeDocumentPosition(organisationID, document)
	createdAt, documentID, err := decodeDocumentPosition(position, organisationID)
	if err != nil || createdAt != "2026-10-01T00:00:00.00000012Z" || documentID != document.Id {
		t.Fatalf("position round trip=%q,%q,%v", createdAt, documentID, err)
	}
	for _, value := range []string{"", position + "=", "malformed", base64.RawURLEncoding.EncodeToString([]byte("document-page-v1|" + organisationID + "|2026-10-01T00:00:00+00:00|" + document.Id)), base64.RawURLEncoding.EncodeToString([]byte("document-page-v2|" + organisationID + "|2026-10-01T00:00:00Z|" + document.Id))} {
		if _, _, err := decodeDocumentPosition(value, organisationID); !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("invalid position %q accepted: %v", value, err)
		}
	}
	if _, _, err := decodeDocumentPosition(position, "018f0000-0000-7000-8000-000000000021"); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("foreign organisation accepted: %v", err)
	}
}
