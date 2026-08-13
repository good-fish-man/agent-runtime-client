package runtime

import (
	"testing"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestFromMetadataMapsPerModelUsage(t *testing.T) {
	protoMetadata := &runtimev1.ResponseMetadata{Model: "gpt-main"}
	protoMetadata.ProtoReflect().SetUnknown(append(
		encodedModelUsage("main", "openai", "gpt-main", 20, 5, 25, 2),
		encodedModelUsage("sub", "openai", "gpt-sub", 8, 2, 10, 1)...,
	))
	metadata := fromMetadata(protoMetadata)
	if metadata == nil || len(metadata.ModelUsage) != 2 {
		t.Fatalf("mapped metadata = %+v", metadata)
	}
	if metadata.ModelUsage[0].ModelID != "main" || metadata.ModelUsage[0].TotalTokens != 25 || metadata.ModelUsage[0].RequestCount != 2 {
		t.Fatalf("main usage = %+v", metadata.ModelUsage[0])
	}
	if metadata.ModelUsage[1].ModelID != "sub" || metadata.ModelUsage[1].TotalTokens != 10 {
		t.Fatalf("sub-agent usage = %+v", metadata.ModelUsage[1])
	}
}

func TestFromMetadataMapsKnownModelUsageField(t *testing.T) {
	protoMetadata := &runtimev1.ResponseMetadata{Model: "gpt-main"}
	message := protoMetadata.ProtoReflect()
	field := message.Descriptor().Fields().ByNumber(12)
	if field == nil {
		t.Skip("compiled Runtime proto does not expose model_usage yet")
	}
	list := message.Mutable(field).List()
	element := list.NewElement()
	usage := element.Message()
	setReflectedString(usage, 1, "main")
	setReflectedString(usage, 2, "openai")
	setReflectedString(usage, 3, "gpt-main")
	setReflectedInt(usage, 4, 12)
	setReflectedInt(usage, 5, 3)
	setReflectedInt(usage, 6, 15)
	setReflectedInt(usage, 7, 1)
	list.Append(element)

	metadata := fromMetadata(protoMetadata)
	if metadata == nil || len(metadata.ModelUsage) != 1 || metadata.ModelUsage[0].ModelID != "main" || metadata.ModelUsage[0].TotalTokens != 15 {
		t.Fatalf("known model usage field = %+v", metadata)
	}
}

func setReflectedString(message protoreflect.Message, number protoreflect.FieldNumber, value string) {
	message.Set(message.Descriptor().Fields().ByNumber(number), protoreflect.ValueOfString(value))
}

func setReflectedInt(message protoreflect.Message, number protoreflect.FieldNumber, value int64) {
	message.Set(message.Descriptor().Fields().ByNumber(number), protoreflect.ValueOfInt32(int32(value)))
}

func encodedModelUsage(modelID, provider, model string, prompt, completion, total, requests uint64) []byte {
	payload := protowire.AppendTag(nil, 1, protowire.BytesType)
	payload = protowire.AppendString(payload, modelID)
	payload = protowire.AppendTag(payload, 2, protowire.BytesType)
	payload = protowire.AppendString(payload, provider)
	payload = protowire.AppendTag(payload, 3, protowire.BytesType)
	payload = protowire.AppendString(payload, model)
	for number, value := range map[protowire.Number]uint64{4: prompt, 5: completion, 6: total, 7: requests} {
		payload = protowire.AppendTag(payload, number, protowire.VarintType)
		payload = protowire.AppendVarint(payload, value)
	}
	result := protowire.AppendTag(nil, 12, protowire.BytesType)
	return protowire.AppendBytes(result, payload)
}
