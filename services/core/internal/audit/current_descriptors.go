package audit

import (
	"sort"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// CurrentDescriptorSet returns exact canonical generated audit descriptors,
// including their dependencies. Historical sets remain independently retained
// under their original fingerprint when the generated schema grows.
func CurrentDescriptorSet() ([]byte, error) {
	seen := make(map[string]*descriptorpb.FileDescriptorProto)
	var visit func(protoreflect.FileDescriptor)
	visit = func(file protoreflect.FileDescriptor) {
		if _, exists := seen[file.Path()]; exists {
			return
		}
		for index := range file.Imports().Len() {
			visit(file.Imports().Get(index).FileDescriptor)
		}
		seen[file.Path()] = protodesc.ToFileDescriptorProto(file)
	}
	visit(tammyv1.File_tammy_v1_audit_proto)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	set := &descriptorpb.FileDescriptorSet{File: make([]*descriptorpb.FileDescriptorProto, 0, len(names))}
	for _, name := range names {
		set.File = append(set.File, seen[name])
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		return nil, ErrDescriptorSetRegistry
	}
	if _, err := validateDescriptorSet(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}
