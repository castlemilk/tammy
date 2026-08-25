package reportingcapability

import (
	"context"
	"errors"
	"reflect"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var ErrReportingCapability = errors.New("reporting capability unavailable")

// RegistryReader resolves one exact reporting capability key.
type RegistryReader interface {
	Lookup(tammyv1.ReportKind, tammyv1.ReportingEntityType, int32) *tammyv1.ReportingCapability
}

// Service exposes the immutable reporting capability registry over Connect.
type Service struct {
	registry RegistryReader
}

var _ tammyv1connect.ReportingCapabilityServiceHandler = (*Service)(nil)

// NewService constructs a read-only reporting capability service.
func NewService(registry RegistryReader) (*Service, error) {
	if nilInterface(registry) {
		return nil, ErrReportingCapability
	}
	return &Service{registry: registry}, nil
}

// GetReportingCapability returns one build-pinned reporting support answer.
func (service *Service) GetReportingCapability(
	_ context.Context,
	request *connect.Request[tammyv1.GetReportingCapabilityRequest],
) (*connect.Response[tammyv1.GetReportingCapabilityResponse], error) {
	if service == nil || nilInterface(service.registry) || request == nil || request.Msg == nil ||
		messageHasUnknown(request.Msg.ProtoReflect()) || protovalidate.Validate(request.Msg) != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrReportingCapability)
	}

	capability := service.registry.Lookup(request.Msg.Report, request.Msg.EntityType, request.Msg.TaxYear)
	if capability == nil || messageHasUnknown(capability.ProtoReflect()) ||
		capability.GetReport() != request.Msg.GetReport() ||
		capability.GetEntityType() != request.Msg.GetEntityType() ||
		capability.GetTaxYear() != request.Msg.GetTaxYear() {
		return nil, connect.NewError(connect.CodeInternal, ErrReportingCapability)
	}
	ownedCapability, ok := proto.Clone(capability).(*tammyv1.ReportingCapability)
	if !ok || ownedCapability == nil {
		return nil, connect.NewError(connect.CodeInternal, ErrReportingCapability)
	}
	response := &tammyv1.GetReportingCapabilityResponse{Capability: ownedCapability}
	if protovalidate.Validate(response) != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrReportingCapability)
	}

	return connect.NewResponse(response), nil
}

func messageHasUnknown(message protoreflect.Message) bool {
	if !message.IsValid() || len(message.GetUnknown()) != 0 {
		return true
	}
	hasUnknown := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.IsMap() {
			if field.MapValue().Kind() != protoreflect.MessageKind && field.MapValue().Kind() != protoreflect.GroupKind {
				return true
			}
			value.Map().Range(func(_ protoreflect.MapKey, entry protoreflect.Value) bool {
				hasUnknown = messageHasUnknown(entry.Message())
				return !hasUnknown
			})
			return !hasUnknown
		}
		if field.IsList() {
			if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
				return true
			}
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if messageHasUnknown(list.Get(index).Message()) {
					hasUnknown = true
					return false
				}
			}
			return true
		}
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			hasUnknown = messageHasUnknown(value.Message())
		}
		return !hasUnknown
	})
	return hasUnknown
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
