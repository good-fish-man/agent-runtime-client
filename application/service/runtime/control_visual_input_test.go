package runtime

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestVisualInputsFromObservationRequiresVisionModel(t *testing.T) {
	data := []byte("image-data")
	sum := sha256.Sum256(data)
	observation := &controlentity.Observation{Attachments: []controlentity.Attachment{{
		ID: "image-1", Kind: "image", MIMEType: "image/png", Size: int64(len(data)),
		SHA256: hex.EncodeToString(sum[:]), Encoding: "base64", Data: base64.StdEncoding.EncodeToString(data),
	}}}
	models := map[string]entity.ModelConfig{"default": {ExtraFields: map[string]any{"capabilities": "chat,tools,vision"}}}
	inputs := visualInputsFromObservation(models, observation)
	if len(inputs) != 1 || string(inputs[0].Data) != string(data) {
		t.Fatalf("visual inputs = %#v", inputs)
	}
	models["default"] = entity.ModelConfig{ExtraFields: map[string]any{"capabilities": "chat,tools"}}
	if inputs := visualInputsFromObservation(models, observation); len(inputs) != 0 {
		t.Fatalf("non-vision model received image: %#v", inputs)
	}
}
