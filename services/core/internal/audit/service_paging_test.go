package audit

import (
	"errors"
	"testing"
)

func TestExportJobPageStartUsesRepositoryOrderRatherThanLexicalIDOrder(t *testing.T) {
	jobs := []ExportJob{{ID: "01890f60-4d6d-7c12-8f02-6c9129d5b099"}, {ID: "01890f60-4d6d-7c12-8f02-6c9129d5b001"}}

	start, err := exportJobPageStart(jobs, jobs[0].ID)
	if err != nil || start != 1 {
		t.Fatalf("page start=%d err=%v, want 1", start, err)
	}
	if _, err := exportJobPageStart(jobs, "01890f60-4d6d-7c12-8f02-6c9129d5b055"); !errors.Is(err, ErrAuditService) {
		t.Fatalf("missing cursor position error=%v, want ErrAuditService", err)
	}
}

func TestExportJobSnapshotBindsEveryListedVersion(t *testing.T) {
	jobs := []ExportJob{{ID: "01890f60-4d6d-7c12-8f02-6c9129d5b001", Version: 1}, {ID: "01890f60-4d6d-7c12-8f02-6c9129d5b002", Version: 1}}
	original := exportJobSnapshot(jobs)
	jobs[0].Version++
	if changed := exportJobSnapshot(jobs); changed == original {
		t.Fatal("snapshot ignored a non-terminal job version change")
	}
}
