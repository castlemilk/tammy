// Package overview owns the read-only cross-module attention projection.
package overview

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrOverview = errors.New("overview: attention summary unavailable")

type Access interface {
	RequireRead(context.Context, *tammyv1.AuthenticationContext) error
}

type SnapshotPort interface {
	Attention(context.Context, string) (Snapshot, error)
}

type RevisionVector struct {
	Financial    uint64
	Ledger       uint64
	Settlement   uint64
	Banking      uint64
	TaxSource    uint64
	Organisation uint64
	Rules        uint64
}

// Snapshot is one immutable module-owned read assembled by a SnapshotPort.
type Snapshot struct {
	DocumentsNeedingReview uint32
	DocumentsReviewed      uint32
	BankingNeedingReview   uint32
	BankingUnreconciled    uint32
	DraftBASWorkpapers     uint32
	BASStatus              tammyv1.BasAttentionStatus
	Items                  []*tammyv1.AttentionItem
	Revisions              RevisionVector
}

type ServiceConfig struct {
	Access    Access
	Snapshots SnapshotPort
}

type Service struct {
	access    Access
	snapshots SnapshotPort
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Access == nil || config.Snapshots == nil {
		return nil, ErrOverview
	}
	return &Service{access: config.Access, snapshots: config.Snapshots}, nil
}

func (service *Service) GetAttentionSummary(
	ctx context.Context,
	request *connect.Request[tammyv1.GetAttentionSummaryRequest],
) (*connect.Response[tammyv1.GetAttentionSummaryResponse], error) {
	if service == nil || service.access == nil || service.snapshots == nil || ctx == nil || request == nil ||
		request.Msg == nil || len(request.Msg.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request.Msg) != nil || !ids.IsCanonicalV7(request.Msg.OrganisationId) ||
		!validPeriod(request.Msg.ReportingPeriod) {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrOverview)
	}
	authentication, ok := proto.Clone(request.Msg.Authentication).(*tammyv1.AuthenticationContext)
	if !ok || authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrOverview)
	}
	if err := service.access.RequireRead(ctx, authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrOverview)
	}
	snapshot, err := service.snapshots.Attention(ctx, request.Msg.OrganisationId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrOverview)
	}
	response := &tammyv1.GetAttentionSummaryResponse{
		DocumentsNeedingReview:            snapshot.DocumentsNeedingReview,
		DocumentsReviewedInPeriod:         snapshot.DocumentsReviewed,
		BankingLinesNeedingReconciliation: snapshot.BankingNeedingReview,
		BankingLinesUnreconciledInPeriod:  snapshot.BankingUnreconciled,
		CurrentDraftBasWorkpapers:         snapshot.DraftBASWorkpapers,
		BasStatus:                         snapshot.BASStatus,
		AttentionItems:                    cloneItems(snapshot.Items),
		Revisions: &tammyv1.AttentionRevisionVector{
			FinancialRevision:           snapshot.Revisions.Financial,
			LedgerRevision:              snapshot.Revisions.Ledger,
			SettlementRevision:          snapshot.Revisions.Settlement,
			BankingRevision:             snapshot.Revisions.Banking,
			TaxSourceRevision:           snapshot.Revisions.TaxSource,
			OrganisationProfileRevision: snapshot.Revisions.Organisation,
			RuleBundleRevision:          snapshot.Revisions.Rules,
		},
		AsOfDate:        proto.Clone(request.Msg.AsOfDate).(*tammyv1.CivilDate),
		ReportingPeriod: proto.Clone(request.Msg.ReportingPeriod).(*tammyv1.ReportingPeriod),
	}
	if protovalidate.Validate(response) != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrOverview)
	}
	return connect.NewResponse(response), nil
}

func validPeriod(period *tammyv1.ReportingPeriod) bool {
	if period == nil || period.StartDate == nil || period.EndDate == nil {
		return false
	}
	start := civilOrdinal(period.StartDate)
	end := civilOrdinal(period.EndDate)
	return start != 0 && end != 0 && start <= end
}

func civilOrdinal(value *tammyv1.CivilDate) int64 {
	if value == nil || value.Year < 1 || value.Year > 9999 || value.Month < 1 || value.Month > 12 ||
		value.Day < 1 || value.Day > 31 {
		return 0
	}
	return int64(value.Year)*10_000 + int64(value.Month)*100 + int64(value.Day)
}

func cloneItems(items []*tammyv1.AttentionItem) []*tammyv1.AttentionItem {
	cloned := make([]*tammyv1.AttentionItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			cloned = append(cloned, nil)
			continue
		}
		cloned = append(cloned, proto.Clone(item).(*tammyv1.AttentionItem))
	}
	return cloned
}
