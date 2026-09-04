package worldmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	knowledgeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
)

const SnapshotSchema = "athena.world-snapshot.v1"

type Store interface {
	SaveObservation(context.Context, controlentity.Observation) error
	FindTask(context.Context, string) (*controlentity.TaskSession, error)
	FindWorldState(context.Context, string) (*controlentity.WorldState, error)
	QueryWorld(context.Context, string, controlentity.WorldQuery) ([]controlentity.WorldEntity, []controlentity.WorldRelation, []controlentity.WorldFact, error)
	RecordWorldConflict(context.Context, controlentity.WorldConflict) error
	ListWorldConflicts(context.Context, string, string, int) ([]controlentity.WorldConflict, error)
	ResolveWorldConflict(context.Context, string, string, string, int64) (*controlentity.WorldConflict, error)
}

type OntologyResolver interface {
	ResolveActiveOntology(context.Context, string) (*knowledgeentity.OntologyPack, *knowledgeentity.OntologyVersion, error)
}

type OntologyContext struct {
	PackID          string                             `json:"pack_id"`
	Version         string                             `json:"version"`
	Checksum        string                             `json:"checksum"`
	Definition      knowledgeentity.OntologyDefinition `json:"definition"`
	ValidationRules []knowledgeentity.ValidationRule   `json:"validation_rules"`
}

type Snapshot = controlentity.WorldSnapshot

// Service is the application-level single writer, ontology gate, entity
// resolver, and immutable snapshot issuer for world state.
type Service struct {
	store     Store
	validator Validator
	mu        sync.RWMutex
	resolver  OntologyResolver
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (a *Service) SetOntologyResolver(resolver OntologyResolver) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.resolver = resolver
	a.mu.Unlock()
}

func (a *Service) CommitObservation(ctx context.Context, observation controlentity.Observation) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("world state authority is unavailable")
	}
	if observation.WorldPatch == nil {
		return a.store.SaveObservation(ctx, observation)
	}
	task, err := a.store.FindTask(ctx, observation.TaskID)
	if err != nil {
		return fmt.Errorf("resolve world owner: %w", err)
	}
	if task == nil || strings.TrimSpace(task.UserID) == "" {
		return fmt.Errorf("world task %q has no authoritative owner", observation.TaskID)
	}
	ontology, err := a.resolveOntology(ctx, task.UserID)
	if err != nil {
		return err
	}
	if observation.WorldPatch.OntologyPack != ontology.PackID || observation.WorldPatch.OntologyVersion != ontology.Version {
		return fmt.Errorf("world patch ontology binding %s@%s does not match active %s@%s", observation.WorldPatch.OntologyPack, observation.WorldPatch.OntologyVersion, ontology.PackID, ontology.Version)
	}
	if err := validatePatchEvidence(observation); err != nil {
		return err
	}
	current, err := a.store.FindWorldState(ctx, observation.TaskID)
	if err != nil {
		return fmt.Errorf("load current world state: %w", err)
	}
	state := map[string]any{}
	if current != nil {
		state = current.State
		if observation.WorldPatch.BaseRevision == 0 {
			patch := *observation.WorldPatch
			patch.BaseRevision = current.Revision
			observation.WorldPatch = &patch
		}
	}
	resolved, conflict, err := a.resolveEntities(ctx, task.UserID, observation, state)
	if err != nil {
		return err
	}
	if conflict != nil {
		if recordErr := a.store.RecordWorldConflict(ctx, *conflict); recordErr != nil {
			return fmt.Errorf("record entity resolution conflict: %w", recordErr)
		}
		return fmt.Errorf("entity resolution conflict %s requires review", conflict.ConflictID)
	}
	observation = resolved
	if err := a.validator.Validate(state, *observation.WorldPatch, ontology); err != nil {
		return fmt.Errorf("ontology validation failed: %w", err)
	}
	return a.store.SaveObservation(ctx, observation)
}

func (a *Service) Snapshot(ctx context.Context, ownerID, taskID string) (*Snapshot, error) {
	if a == nil || a.store == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	taskID = strings.TrimSpace(taskID)
	if ownerID == "" || taskID == "" {
		return nil, fmt.Errorf("world snapshot requires owner and task ids")
	}
	task, err := a.store.FindTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("resolve world task: %w", err)
	}
	if task != nil && task.UserID != ownerID {
		return nil, fmt.Errorf("world task %q does not belong to the authenticated owner", taskID)
	}
	state, err := a.store.FindWorldState(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load world state: %w", err)
	}
	if task == nil && state != nil {
		return nil, fmt.Errorf("world state %q has no owning task", taskID)
	}
	pack, ontology, err := a.resolveOntologyWithPack(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	revision := int64(0)
	updatedAt := time.Time{}
	if state != nil {
		revision = state.Revision
		updatedAt = state.UpdatedAt
	}
	query := controlentity.WorldQuery{Schema: "athena.world-query.v1", TaskID: taskID, AsOf: time.Now().UTC(), Limit: 500}
	entities, relations, facts, err := a.store.QueryWorld(ctx, ownerID, query)
	if err != nil {
		return nil, fmt.Errorf("query structured world: %w", err)
	}
	sortWorld(entities, relations, facts)
	capturedAt := updatedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	snapshot := &Snapshot{
		Schema: SnapshotSchema, TaskID: taskID, Revision: revision, OntologyPack: pack.PackID,
		OntologyVersion: ontology.Version, OntologyChecksum: ontology.Checksum,
		Entities: entities, Relations: relations, Facts: facts, CapturedAt: capturedAt,
	}
	checksum, err := snapshotChecksum(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.Checksum = checksum
	snapshot.SnapshotID = "snapshot-" + checksum[:24]
	return snapshot, nil
}

func (a *Service) OntologyContext(ctx context.Context, ownerID string) (*OntologyContext, error) {
	pack, ontology, err := a.resolveOntologyWithPack(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	return &OntologyContext{PackID: pack.PackID, Version: ontology.Version, Checksum: ontology.Checksum, Definition: ontology.Definition, ValidationRules: ontology.ValidationRules}, nil
}

func (a *Service) Query(ctx context.Context, ownerID string, query controlentity.WorldQuery) (*Snapshot, error) {
	query.Normalize()
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if query.TaskID != "" {
		return a.Snapshot(ctx, ownerID, query.TaskID)
	}
	pack, ontology, err := a.resolveOntologyWithPack(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	entities, relations, facts, err := a.store.QueryWorld(ctx, ownerID, query)
	if err != nil {
		return nil, err
	}
	sortWorld(entities, relations, facts)
	snapshot := &Snapshot{Schema: SnapshotSchema, Revision: maxWorldRevision(entities, relations, facts), OntologyPack: pack.PackID, OntologyVersion: ontology.Version, OntologyChecksum: ontology.Checksum, Entities: entities, Relations: relations, Facts: facts, CapturedAt: query.AsOf}
	checksum, err := snapshotChecksum(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.Checksum = checksum
	snapshot.SnapshotID = "snapshot-" + checksum[:24]
	return snapshot, nil
}

func (a *Service) Conflicts(ctx context.Context, ownerID, status string, limit int) ([]controlentity.WorldConflict, error) {
	return a.store.ListWorldConflicts(ctx, ownerID, strings.ToUpper(strings.TrimSpace(status)), limit)
}
func (a *Service) ResolveConflict(ctx context.Context, ownerID, conflictID, resolution string, revision int64) (*controlentity.WorldConflict, error) {
	if strings.TrimSpace(resolution) == "" {
		return nil, fmt.Errorf("world conflict resolution is required")
	}
	return a.store.ResolveWorldConflict(ctx, ownerID, conflictID, resolution, revision)
}

func (a *Service) resolveOntology(ctx context.Context, ownerID string) (*knowledgeentity.OntologyVersion, error) {
	_, ontology, err := a.resolveOntologyWithPack(ctx, ownerID)
	return ontology, err
}

func (a *Service) resolveOntologyWithPack(ctx context.Context, ownerID string) (*knowledgeentity.OntologyPack, *knowledgeentity.OntologyVersion, error) {
	a.mu.RLock()
	resolver := a.resolver
	a.mu.RUnlock()
	if resolver == nil {
		return nil, nil, fmt.Errorf("active ontology resolver is unavailable")
	}
	pack, ontology, err := resolver.ResolveActiveOntology(ctx, ownerID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve active ontology: %w", err)
	}
	if pack == nil || ontology == nil || pack.PackID != ontology.PackID || pack.Current != ontology.Version {
		return nil, nil, fmt.Errorf("active ontology binding is inconsistent")
	}
	if err := validateOntology(ontology); err != nil {
		return nil, nil, err
	}
	return pack, ontology, nil
}

func snapshotChecksum(snapshot *Snapshot) (string, error) {
	payload := *snapshot
	payload.Checksum, payload.SnapshotID = "", ""
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode world snapshot checksum: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func validatePatchEvidence(observation controlentity.Observation) error {
	available := map[string]bool{}
	for _, item := range observation.Evidence {
		available[item.EvidenceID] = true
	}
	for _, item := range observation.Attachments {
		available[item.ID] = true
	}
	for _, item := range observation.WorldPatch.Evidence {
		if strings.TrimSpace(item.EvidenceID) == "" || !available[item.EvidenceID] {
			return fmt.Errorf("world patch evidence %q is not attached to the observation", item.EvidenceID)
		}
	}
	return nil
}

func (a *Service) resolveEntities(ctx context.Context, ownerID string, observation controlentity.Observation, state map[string]any) (controlentity.Observation, *controlentity.WorldConflict, error) {
	patch := *observation.WorldPatch
	patch.Mutations = append([]controlentity.WorldMutation(nil), patch.Mutations...)
	query := controlentity.WorldQuery{Schema: "athena.world-query.v1", TaskID: observation.TaskID, AsOf: time.Now().UTC(), Limit: 500, IncludeExpired: false}
	existing, _, _, err := a.store.QueryWorld(ctx, ownerID, query)
	if err != nil {
		return observation, nil, fmt.Errorf("entity resolution query: %w", err)
	}
	replacements := map[string]string{}
	for _, mutation := range patch.Mutations {
		parts, parseErr := jsonPointerParts(mutation.Path)
		if parseErr != nil || len(parts) < 2 || parts[0] != "entities" || mutation.Operation == "remove" {
			continue
		}
		object, ok := mutation.Value.(map[string]any)
		if !ok {
			continue
		}
		name := firstString(object, "canonical_name", "name")
		kind := stringField(object, "type")
		if name == "" || kind == "" {
			continue
		}
		matches := make([]string, 0)
		key := normalizeIdentity(name)
		for _, candidate := range existing {
			if candidate.Type != kind {
				continue
			}
			names := append([]string{candidate.CanonicalName}, candidate.Aliases...)
			for _, value := range names {
				if normalizeIdentity(value) == key {
					matches = append(matches, candidate.EntityID)
					break
				}
			}
		}
		sort.Strings(matches)
		if len(matches) > 1 {
			now := time.Now().UTC()
			conflict := &controlentity.WorldConflict{ConflictID: controlentity.NewID("world-conflict"), OwnerID: ownerID, TaskID: observation.TaskID, ObservationID: observation.ObservationID, Kind: "ENTITY_RESOLUTION", Subject: name, CandidateIDs: matches, Details: map[string]any{"entity_type": kind, "requested_id": parts[1]}, Status: "OPEN", Revision: 1, CreatedAt: now, UpdatedAt: now}
			return observation, conflict, nil
		}
		if len(matches) == 1 && matches[0] != parts[1] {
			replacements[parts[1]] = matches[0]
		}
	}
	if len(replacements) > 0 {
		for i := range patch.Mutations {
			patch.Mutations[i] = rewriteMutationEntityIDs(patch.Mutations[i], replacements)
		}
	}
	observation.WorldPatch = &patch
	return observation, nil, nil
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := stringField(value, key); result != "" {
			return result
		}
	}
	return ""
}
func normalizeIdentity(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func rewriteMutationEntityIDs(mutation controlentity.WorldMutation, replacements map[string]string) controlentity.WorldMutation {
	for from, to := range replacements {
		mutation.Path = strings.Replace(mutation.Path, "/entities/"+from, "/entities/"+to, 1)
	}
	object, ok := mutation.Value.(map[string]any)
	if ok {
		cloned, _ := cloneState(object)
		for _, key := range []string{"source", "source_id", "target", "target_id", "subject_id"} {
			if value, ok := cloned[key].(string); ok {
				if replacement, exists := replacements[value]; exists {
					cloned[key] = replacement
				}
			}
		}
		mutation.Value = cloned
	}
	return mutation
}
func maxWorldRevision(entities []controlentity.WorldEntity, relations []controlentity.WorldRelation, facts []controlentity.WorldFact) int64 {
	var result int64
	for _, v := range entities {
		if v.Revision > result {
			result = v.Revision
		}
	}
	for _, v := range relations {
		if v.Revision > result {
			result = v.Revision
		}
	}
	for _, v := range facts {
		if v.Revision > result {
			result = v.Revision
		}
	}
	return result
}

func sortWorld(entities []controlentity.WorldEntity, relations []controlentity.WorldRelation, facts []controlentity.WorldFact) {
	sort.Slice(entities, func(i, j int) bool { return entities[i].EntityID < entities[j].EntityID })
	sort.Slice(relations, func(i, j int) bool { return relations[i].RelationID < relations[j].RelationID })
	sort.Slice(facts, func(i, j int) bool { return facts[i].FactID < facts[j].FactID })
}
