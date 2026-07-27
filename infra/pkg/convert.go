// Package infrapkg holds low-level conversion helpers shared by the infra gRPC
// gateway: protobuf Struct/Value <-> Go maps, and enum string <-> proto mapping.
package infrapkg

import (
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"
)

// ---- structpb helpers ----

// ToStruct converts a Go map to a protobuf Struct. Returns nil for empty maps or
// on conversion error (best-effort; unsupported values are dropped by returning nil).
func ToStruct(m map[string]any) *structpb.Struct {
	if len(m) == 0 {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}

// FromStruct converts a protobuf Struct to a Go map (nil-safe).
func FromStruct(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}

// ToValue converts an arbitrary Go value to a protobuf Value (nil-safe).
func ToValue(v any) *structpb.Value {
	if v == nil {
		return nil
	}
	val, err := structpb.NewValue(v)
	if err != nil {
		return nil
	}
	return val
}

// FromValue converts a protobuf Value to a Go value (nil-safe).
func FromValue(v *structpb.Value) any {
	if v == nil {
		return nil
	}
	return v.AsInterface()
}

// StructsToMaps converts a slice of protobuf Structs to Go maps.
func StructsToMaps(list []*structpb.Struct) []map[string]any {
	if len(list) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, FromStruct(s))
	}
	return out
}

// ---- enum helpers ----

// RiskLevelToProto maps a risk-level name to its proto enum. Accepts short
// ("low") or full ("RISK_LEVEL_LOW") forms, case-insensitive.
func RiskLevelToProto(s string) runtimev1.RiskLevel {
	switch normalizeEnum(s) {
	case "LOW":
		return runtimev1.RiskLevel_RISK_LEVEL_LOW
	case "MEDIUM":
		return runtimev1.RiskLevel_RISK_LEVEL_MEDIUM
	case "HIGH":
		return runtimev1.RiskLevel_RISK_LEVEL_HIGH
	default:
		return runtimev1.RiskLevel_RISK_LEVEL_UNSPECIFIED
	}
}

// RiskLevelFromProto maps a proto risk-level enum to a short name.
func RiskLevelFromProto(r runtimev1.RiskLevel) string {
	switch r {
	case runtimev1.RiskLevel_RISK_LEVEL_LOW:
		return "low"
	case runtimev1.RiskLevel_RISK_LEVEL_MEDIUM:
		return "medium"
	case runtimev1.RiskLevel_RISK_LEVEL_HIGH:
		return "high"
	default:
		return ""
	}
}

// ModelRoleToProto maps a model-role name to its proto enum.
func ModelRoleToProto(s string) runtimev1.ModelRole {
	switch normalizeEnum(s) {
	case "DEFAULT":
		return runtimev1.ModelRole_MODEL_ROLE_DEFAULT
	case "REWRITE":
		return runtimev1.ModelRole_MODEL_ROLE_REWRITE
	case "SKILL":
		return runtimev1.ModelRole_MODEL_ROLE_SKILL
	case "SUMMARIZE":
		return runtimev1.ModelRole_MODEL_ROLE_SUMMARIZE
	default:
		return runtimev1.ModelRole_MODEL_ROLE_UNSPECIFIED
	}
}

// ModelRoleFromProto maps a proto model-role enum to a short name.
func ModelRoleFromProto(r runtimev1.ModelRole) string {
	switch r {
	case runtimev1.ModelRole_MODEL_ROLE_DEFAULT:
		return "default"
	case runtimev1.ModelRole_MODEL_ROLE_REWRITE:
		return "rewrite"
	case runtimev1.ModelRole_MODEL_ROLE_SKILL:
		return "skill"
	case runtimev1.ModelRole_MODEL_ROLE_SUMMARIZE:
		return "summarize"
	default:
		return ""
	}
}

// MemoryTypeToProto maps a memory-type name to its proto enum.
func MemoryTypeToProto(s string) runtimev1.MemoryType {
	switch normalizeEnum(s) {
	case "USER":
		return runtimev1.MemoryType_MEMORY_TYPE_USER
	case "FEEDBACK":
		return runtimev1.MemoryType_MEMORY_TYPE_FEEDBACK
	case "PROJECT":
		return runtimev1.MemoryType_MEMORY_TYPE_PROJECT
	case "REFERENCE":
		return runtimev1.MemoryType_MEMORY_TYPE_REFERENCE
	default:
		return runtimev1.MemoryType_MEMORY_TYPE_UNSPECIFIED
	}
}

// MemoryTypeFromProto maps a proto memory-type enum to a short name.
func MemoryTypeFromProto(t runtimev1.MemoryType) string {
	switch t {
	case runtimev1.MemoryType_MEMORY_TYPE_USER:
		return "user"
	case runtimev1.MemoryType_MEMORY_TYPE_FEEDBACK:
		return "feedback"
	case runtimev1.MemoryType_MEMORY_TYPE_PROJECT:
		return "project"
	case runtimev1.MemoryType_MEMORY_TYPE_REFERENCE:
		return "reference"
	default:
		return ""
	}
}

// ServingStatusFromProto maps the health serving-status enum to a name.
func ServingStatusFromProto(st runtimev1.HealthCheckResponse_ServingStatus) string {
	switch st {
	case runtimev1.HealthCheckResponse_SERVING:
		return "SERVING"
	case runtimev1.HealthCheckResponse_NOT_SERVING:
		return "NOT_SERVING"
	default:
		return "UNKNOWN"
	}
}

// normalizeEnum upper-cases, trims, and strips a common enum prefix so both
// "low" and "RISK_LEVEL_LOW" resolve to "LOW".
func normalizeEnum(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	for _, p := range []string{"RISK_LEVEL_", "MODEL_ROLE_", "MEMORY_TYPE_"} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}
