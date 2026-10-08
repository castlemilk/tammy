package purchases

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func purchaseResultFits(value proto.Message) bool {
	frame, err := anypb.New(value)
	return err == nil && proto.Size(frame) <= MaximumListResponseBytes
}
