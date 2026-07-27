package agent

import (
	"encoding/json"
	"strings"

	"github.com/jinzhu/copier"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/agent"
)

// SysAgentAssembler converts between DTOs and the domain entity.
type SysAgentAssembler struct{}

// NewSysAgentAssembler builds the assembler.
func NewSysAgentAssembler() *SysAgentAssembler { return &SysAgentAssembler{} }

func (a *SysAgentAssembler) D2ECreate(d *dto.CreateSysAgentReq) *entity.SysAgent {
	var en entity.SysAgent
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	return &en
}

func (a *SysAgentAssembler) D2EDelete(d *dto.DelSysAgentReq) *entity.SysAgent {
	return &entity.SysAgent{Ulid: d.Ulid}
}

func (a *SysAgentAssembler) D2EUpdate(d *dto.UpdateSysAgentReq) *entity.SysAgent {
	var en entity.SysAgent
	if err := copier.Copy(&en, d); err != nil {
		panic(err)
	}
	if d.Enabled != nil {
		en.Enabled = *d.Enabled
	}
	if d.IsPeriodic != nil {
		en.IsPeriodic = *d.IsPeriodic
	}
	return &en
}

func (a *SysAgentAssembler) E2DFind(en *entity.SysAgent) *dto.FindSysAgentRsp {
	var d dto.FindSysAgentRsp
	if err := copier.Copy(&d, en); err != nil {
		panic(err)
	}
	d.Config = scrubSensitiveAgentConfigJSON(d.Config)
	d.ConfigJson = scrubSensitiveAgentConfigJSON(d.ConfigJson)
	return &d
}

func (a *SysAgentAssembler) E2DList(ens []*entity.SysAgent) []*dto.FindSysAgentRsp {
	out := make([]*dto.FindSysAgentRsp, 0, len(ens))
	for _, en := range ens {
		out = append(out, a.E2DFind(en))
	}
	return out
}

func scrubSensitiveAgentConfigJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	scrubSensitiveAgentConfig(value)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(out)
}

func scrubSensitiveAgentConfig(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if isSensitiveAgentConfigKey(key) {
				delete(v, key)
				continue
			}
			scrubSensitiveAgentConfig(child)
		}
	case []any:
		for _, child := range v {
			scrubSensitiveAgentConfig(child)
		}
	}
}

func isSensitiveAgentConfigKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	switch normalized {
	case "apikey", "systemprompt", "rewriteprompt", "summarizeprompt", "prompt", "instruction":
		return true
	default:
		return false
	}
}
