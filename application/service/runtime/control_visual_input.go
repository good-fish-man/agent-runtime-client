package runtime

import (
	"encoding/base64"
	"strings"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func visualInputsFromObservation(models map[string]entity.ModelConfig, observation *controlentity.Observation) []entity.VisualInput {
	if observation == nil || !defaultModelSupportsVision(models) {
		return nil
	}
	result := make([]entity.VisualInput, 0, len(observation.Attachments))
	for _, attachment := range observation.Attachments {
		if attachment.Kind != "image" || attachment.Data == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(attachment.Data)
		if err != nil || len(data) == 0 || len(data) > controlentity.MaxAttachmentBytes {
			continue
		}
		result = append(result, entity.VisualInput{
			ID: attachment.ID, MIMEType: attachment.MIMEType, Data: data, SHA256: attachment.SHA256,
			Purpose: attachment.Purpose, Detail: attachment.Detail,
		})
	}
	return result
}

func defaultModelSupportsVision(models map[string]entity.ModelConfig) bool {
	model, ok := models["default"]
	if !ok {
		return false
	}
	capabilities := ""
	if model.ExtraFields != nil {
		capabilities, _ = model.ExtraFields["capabilities"].(string)
	}
	for _, capability := range strings.FieldsFunc(strings.ToLower(capabilities), func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' '
	}) {
		if capability == "vision" || capability == "multimodal" || capability == "image-input" {
			return true
		}
	}
	return false
}
