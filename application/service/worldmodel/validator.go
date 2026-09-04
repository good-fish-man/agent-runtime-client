package worldmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	knowledgeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	knowledgev1 "github.com/good-fish-man/athena-protocol/protocol/knowledge/v1"
)

// Validator applies ontology constraints to the semantic portion of a world
// patch. Non-semantic roots remain available for operational device state.
type Validator struct{}

// ValidateSnapshot verifies a read-only external projection against the same
// ontology rules used for durable patches. External providers never bypass the
// local authority merely because their transport is trusted.
func (Validator) ValidateSnapshot(snapshot *controlentity.WorldSnapshot, ontology *knowledgeentity.OntologyVersion) error {
	if snapshot == nil {
		return fmt.Errorf("world snapshot is required")
	}
	if snapshot.Schema != SnapshotSchema {
		return fmt.Errorf("world snapshot requires schema %q", SnapshotSchema)
	}
	if err := validateOntology(ontology); err != nil {
		return err
	}
	if snapshot.OntologyPack != ontology.PackID || snapshot.OntologyVersion != ontology.Version {
		return fmt.Errorf("snapshot ontology binding does not match the active ontology")
	}
	state := map[string]any{"entities": map[string]any{}, "relations": map[string]any{}, "facts": map[string]any{}}
	entities := state["entities"].(map[string]any)
	for _, value := range snapshot.Entities {
		if value.EntityID == "" || value.Confidence <= 0 || value.Confidence > 1 || len(value.Evidence) == 0 || !value.ExpiresAt.After(value.ObservedAt) {
			return fmt.Errorf("external entity has invalid identity, provenance, confidence, or lifetime")
		}
		if _, exists := entities[value.EntityID]; exists {
			return fmt.Errorf("external snapshot contains duplicate entity %q", value.EntityID)
		}
		object := cloneObject(value.Properties)
		object["type"], object["canonical_name"], object["aliases"] = value.Type, value.CanonicalName, value.Aliases
		entities[value.EntityID] = object
	}
	relations := state["relations"].(map[string]any)
	for _, value := range snapshot.Relations {
		if value.RelationID == "" || value.Confidence <= 0 || value.Confidence > 1 || len(value.Evidence) == 0 || !value.ExpiresAt.After(value.ObservedAt) {
			return fmt.Errorf("external relation has invalid identity, provenance, confidence, or lifetime")
		}
		if _, exists := relations[value.RelationID]; exists {
			return fmt.Errorf("external snapshot contains duplicate relation %q", value.RelationID)
		}
		object := cloneObject(value.Properties)
		object["source_id"], object["target_id"], object["predicate"] = value.SourceID, value.TargetID, value.Predicate
		relations[value.RelationID] = object
	}
	facts := state["facts"].(map[string]any)
	for _, value := range snapshot.Facts {
		if value.FactID == "" || value.Confidence <= 0 || value.Confidence > 1 || len(value.Evidence) == 0 || !value.ExpiresAt.After(value.ObservedAt) {
			return fmt.Errorf("external fact has invalid identity, provenance, confidence, or lifetime")
		}
		if _, exists := facts[value.FactID]; exists {
			return fmt.Errorf("external snapshot contains duplicate fact %q", value.FactID)
		}
		object := cloneObject(value.Properties)
		object["subject_id"], object["subject_type"], object["predicate"], object["value"], object["value_type"] = value.SubjectID, value.SubjectType, value.Predicate, value.Value, value.ValueType
		facts[value.FactID] = object
	}
	for id := range entities {
		if err := validateEntity(state, id, ontology); err != nil {
			return err
		}
	}
	for id := range relations {
		if err := validateRelation(state, id, ontology); err != nil {
			return err
		}
	}
	for id := range facts {
		if err := validateFact(state, id, ontology); err != nil {
			return err
		}
	}
	return nil
}

func cloneObject(value map[string]any) map[string]any {
	result := make(map[string]any, len(value)+4)
	for key, item := range value {
		result[key] = item
	}
	return result
}

func (Validator) Validate(state map[string]any, patch controlentity.WorldPatch, ontology *knowledgeentity.OntologyVersion) error {
	if err := patch.Validate(); err != nil {
		return err
	}
	if err := validateOntology(ontology); err != nil {
		return err
	}
	working, err := cloneState(state)
	if err != nil {
		return fmt.Errorf("clone world state: %w", err)
	}
	touchedEntities := map[string]struct{}{}
	touchedRelations := map[string]struct{}{}
	touchedFacts := map[string]struct{}{}
	for index, mutation := range patch.Mutations {
		parts, err := jsonPointerParts(mutation.Path)
		if err != nil {
			return fmt.Errorf("world mutation %d: %w", index, err)
		}
		if len(parts) == 0 || parts[0] == "" {
			return fmt.Errorf("world mutation %d cannot replace the root", index)
		}
		if len(parts) > 32 {
			return fmt.Errorf("world mutation %d exceeds the maximum path depth", index)
		}
		switch parts[0] {
		case "entities":
			if len(parts) < 2 || parts[1] == "" {
				return fmt.Errorf("world mutation %d must address a specific ontology entity", index)
			}
			touchedEntities[parts[1]] = struct{}{}
		case "relations":
			if len(parts) < 2 || parts[1] == "" {
				return fmt.Errorf("world mutation %d must address a specific ontology relation", index)
			}
			touchedRelations[parts[1]] = struct{}{}
		case "facts":
			if len(parts) < 2 || parts[1] == "" {
				return fmt.Errorf("world mutation %d must address a specific ontology fact", index)
			}
			touchedFacts[parts[1]] = struct{}{}
		}
		if err := applyMutation(working, mutation, parts); err != nil {
			return fmt.Errorf("world mutation %d: %w", index, err)
		}
	}
	for id := range touchedEntities {
		if err := validateEntity(working, id, ontology); err != nil {
			return err
		}
	}
	for id := range touchedRelations {
		if err := validateRelation(working, id, ontology); err != nil {
			return err
		}
	}
	for id := range touchedFacts {
		if err := validateFact(working, id, ontology); err != nil {
			return err
		}
	}
	return nil
}

func validateOntology(ontology *knowledgeentity.OntologyVersion) error {
	if ontology == nil {
		return fmt.Errorf("active ontology is unavailable")
	}
	if ontology.Status != knowledgev1.OntologyApproved && ontology.Status != knowledgev1.OntologyApplied {
		return fmt.Errorf("ontology %s@%s is not approved", ontology.PackID, ontology.Version)
	}
	if err := ontology.Validate(); err != nil {
		return fmt.Errorf("active ontology is invalid: %w", err)
	}
	body, err := json.Marshal(ontology.Definition)
	if err != nil {
		return fmt.Errorf("encode active ontology: %w", err)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != ontology.Checksum {
		return fmt.Errorf("active ontology checksum mismatch")
	}
	return nil
}

func validateEntity(state map[string]any, id string, ontology *knowledgeentity.OntologyVersion) error {
	value, exists := semanticValue(state, "entities", id)
	if !exists {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("ontology entity %q must be an object", id)
	}
	typeName := stringField(object, "type")
	if typeName == "" {
		return fmt.Errorf("ontology entity %q requires a type", id)
	}
	for _, definition := range ontology.Definition.Entities {
		if definition.ID == typeName {
			return nil
		}
	}
	return fmt.Errorf("ontology entity %q uses unknown type %q", id, typeName)
}

func validateRelation(state map[string]any, id string, ontology *knowledgeentity.OntologyVersion) error {
	value, exists := semanticValue(state, "relations", id)
	if !exists {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("ontology relation %q must be an object", id)
	}
	predicate := stringField(object, "predicate")
	if predicate == "" {
		predicate = stringField(object, "type")
	}
	relation, ok := ontologyRelation(ontology, predicate)
	if !ok {
		return fmt.Errorf("ontology relation %q uses unknown predicate %q", id, predicate)
	}
	sourceType := endpointType(state, object, "source")
	targetType := endpointType(state, object, "target")
	if sourceType == "" || targetType == "" {
		return fmt.Errorf("ontology relation %q requires resolvable source and target types", id)
	}
	if sourceType != relation.SourceType || targetType != relation.TargetType {
		return fmt.Errorf("ontology relation %q expects %s -> %s, got %s -> %s", id, relation.SourceType, relation.TargetType, sourceType, targetType)
	}
	return nil
}

func validateFact(state map[string]any, id string, ontology *knowledgeentity.OntologyVersion) error {
	value, exists := semanticValue(state, "facts", id)
	if !exists {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("ontology fact %q must be an object", id)
	}
	subjectType := stringField(object, "subject_type")
	predicate := stringField(object, "predicate")
	valueType := stringField(object, "value_type")
	if subjectType == "" || predicate == "" || valueType == "" {
		return fmt.Errorf("ontology fact %q requires subject_type, predicate, and value_type", id)
	}
	for _, rule := range ontology.ValidationRules {
		if rule.SubjectType == subjectType && rule.Predicate == predicate && rule.ValueType == valueType {
			return nil
		}
	}
	if relation, ok := ontologyRelation(ontology, predicate); ok && relation.SourceType == subjectType && relation.TargetType == valueType {
		return nil
	}
	return fmt.Errorf("ontology fact %q is not permitted by %s@%s", id, ontology.PackID, ontology.Version)
}

func ontologyRelation(ontology *knowledgeentity.OntologyVersion, predicate string) (knowledgev1.OntologyRelation, bool) {
	for _, relation := range ontology.Definition.Relations {
		if relation.ID == predicate {
			return relation, true
		}
	}
	return knowledgev1.OntologyRelation{}, false
}

func endpointType(state map[string]any, relation map[string]any, endpoint string) string {
	if value := stringField(relation, endpoint+"_type"); value != "" {
		return value
	}
	id := stringField(relation, endpoint+"_id")
	if id == "" {
		id = stringField(relation, endpoint)
	}
	value, ok := semanticValue(state, "entities", id)
	if !ok {
		return ""
	}
	object, _ := value.(map[string]any)
	return stringField(object, "type")
}

func semanticValue(state map[string]any, root, id string) (any, bool) {
	values, ok := state[root].(map[string]any)
	if !ok {
		return nil, false
	}
	value, exists := values[id]
	return value, exists
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func cloneState(state map[string]any) (map[string]any, error) {
	if state == nil {
		return map[string]any{}, nil
	}
	body, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func applyMutation(state map[string]any, mutation controlentity.WorldMutation, parts []string) error {
	cursor := state
	for _, part := range parts[:len(parts)-1] {
		value, exists := cursor[part]
		if !exists || value == nil {
			child := map[string]any{}
			cursor[part] = child
			cursor = child
			continue
		}
		child, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("path %s traverses non-object value at %q", mutation.Path, part)
		}
		cursor = child
	}
	leaf := parts[len(parts)-1]
	switch mutation.Operation {
	case "set":
		cursor[leaf] = mutation.Value
	case "remove":
		delete(cursor, leaf)
	case "merge":
		incoming, ok := mutation.Value.(map[string]any)
		if !ok {
			return fmt.Errorf("merge mutation at %s requires an object", mutation.Path)
		}
		target, exists := cursor[leaf]
		if !exists || target == nil {
			target = map[string]any{}
			cursor[leaf] = target
		}
		object, ok := target.(map[string]any)
		if !ok {
			return fmt.Errorf("merge mutation at %s targets a non-object value", mutation.Path)
		}
		for key, value := range incoming {
			object[key] = value
		}
	}
	return nil
}

func jsonPointerParts(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("world mutation path must be an absolute JSON pointer")
	}
	raw := strings.Split(strings.TrimPrefix(path, "/"), "/")
	parts := make([]string, len(raw))
	for index, token := range raw {
		var out strings.Builder
		for offset := 0; offset < len(token); offset++ {
			if token[offset] != '~' {
				out.WriteByte(token[offset])
				continue
			}
			if offset+1 >= len(token) || (token[offset+1] != '0' && token[offset+1] != '1') {
				return nil, fmt.Errorf("world mutation path contains an invalid JSON pointer escape")
			}
			offset++
			if token[offset] == '0' {
				out.WriteByte('~')
			} else {
				out.WriteByte('/')
			}
		}
		parts[index] = out.String()
	}
	return parts, nil
}
