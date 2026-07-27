package model

import (
	"testing"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
)

func TestD2EUpdateCanClearKeyBinding(t *testing.T) {
	empty := ""
	got := NewSysModelAssembler().D2EUpdate(&dto.UpdateSysModelReq{KeyID: &empty})
	if got.KeyID != "" {
		t.Fatalf("KeyID = %q, want empty", got.KeyID)
	}
}
