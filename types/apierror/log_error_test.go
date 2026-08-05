package apierror

import (
	"testing"

	log "github.com/good-fish-man/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFromErrorPreservesWrappedAPIError(t *testing.T) {
	source := ErrBadRequest.WithMessage("invalid model")
	got := FromError(log.WrapError(source, "SysModelService.Create"))

	if got.Code != source.Code || got.HTTPStatus != source.HTTPStatus || got.Message != source.Message {
		t.Fatalf("FromError() = %#v, want %#v", got, source)
	}
}

func TestFromErrorPreservesWrappedGRPCStatus(t *testing.T) {
	got := FromError(log.WrapError(status.Error(codes.Unavailable, "runtime offline"), "RuntimeGateway.Run"))

	if got.Code != ErrRuntimeUnavailable.Code || got.HTTPStatus != ErrRuntimeUnavailable.HTTPStatus {
		t.Fatalf("FromError() = %#v, want unavailable error", got)
	}
}
