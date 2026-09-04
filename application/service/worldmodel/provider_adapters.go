package worldmodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
)

const maxProviderResponseBytes = 4 << 20

type athenaHTTPProvider struct{}
type sparqlProvider struct{}
type neo4jProvider struct{}
type typeDBProvider struct{}

func adapterFor(provider controlentity.WorldProvider) externalProvider {
	switch provider.Kind {
	case controlentity.WorldProviderSPARQL:
		return sparqlProvider{}
	case controlentity.WorldProviderNeo4j:
		return neo4jProvider{}
	case controlentity.WorldProviderTypeDB:
		return typeDBProvider{}
	default:
		return athenaHTTPProvider{}
	}
}

func (athenaHTTPProvider) Health(ctx context.Context, provider controlentity.WorldProvider) error {
	query := controlentity.WorldQuery{Schema: "athena.world-query.v1", AsOf: time.Now().UTC(), Limit: 1}
	_, _, err := athenaHTTPProvider{}.Query(ctx, provider, query)
	return err
}

func (athenaHTTPProvider) Query(ctx context.Context, provider controlentity.WorldProvider, query controlentity.WorldQuery) (*controlentity.WorldSnapshot, []string, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, nil, err
	}
	response, err := providerRequest(ctx, provider, http.MethodPost, provider.Endpoint, "application/json", body)
	if err != nil {
		return nil, nil, err
	}
	response = unwrapAPIData(response)
	var snapshot controlentity.WorldSnapshot
	if err := json.Unmarshal(response, &snapshot); err != nil {
		return nil, nil, fmt.Errorf("decode Athena world snapshot: %w", err)
	}
	decorateSnapshot(&snapshot, provider, query.AsOf)
	return &snapshot, nil, nil
}

func (sparqlProvider) Health(ctx context.Context, provider controlentity.WorldProvider) error {
	values := url.Values{"query": {"ASK { ?s ?p ?o }"}}
	_, err := providerRequest(ctx, provider, http.MethodPost, provider.Endpoint, "application/x-www-form-urlencoded", []byte(values.Encode()))
	return err
}

func (sparqlProvider) Query(ctx context.Context, provider controlentity.WorldProvider, query controlentity.WorldQuery) (*controlentity.WorldSnapshot, []string, error) {
	statement := boundedTemplate(provider.QueryTemplate, query.Limit)
	if !readOnlySPARQL(statement) {
		return nil, nil, fmt.Errorf("SPARQL provider accepts SELECT queries only")
	}
	values := url.Values{"query": {statement}}
	response, err := providerRequest(ctx, provider, http.MethodPost, provider.Endpoint, "application/x-www-form-urlencoded", []byte(values.Encode()))
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		Results struct {
			Bindings []map[string]struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode SPARQL JSON results: %w", err)
	}
	records := make([]map[string]any, 0, len(payload.Results.Bindings))
	for _, binding := range payload.Results.Bindings {
		record := make(map[string]any, len(binding))
		for key, value := range binding {
			record[key] = value.Value
		}
		records = append(records, record)
	}
	return snapshotFromRecords(provider, query, records)
}

func (neo4jProvider) Health(ctx context.Context, provider controlentity.WorldProvider) error {
	body, _ := json.Marshal(map[string]any{"statement": "RETURN 1 AS ok", "parameters": map[string]any{}})
	_, err := providerRequest(ctx, provider, http.MethodPost, neo4jEndpoint(provider), "application/json", body)
	return err
}

func (neo4jProvider) Query(ctx context.Context, provider controlentity.WorldProvider, query controlentity.WorldQuery) (*controlentity.WorldSnapshot, []string, error) {
	statement := boundedTemplate(provider.QueryTemplate, query.Limit)
	if !readOnlyCypher(statement) {
		return nil, nil, fmt.Errorf("Neo4j provider query must be a read-only MATCH/OPTIONAL MATCH statement")
	}
	body, _ := json.Marshal(map[string]any{"statement": statement, "parameters": map[string]any{"text": query.Text, "limit": query.Limit}, "maxExecutionTime": max(1, provider.TimeoutMS/1000)})
	response, err := providerRequest(ctx, provider, http.MethodPost, neo4jEndpoint(provider), "application/json", body)
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		Data struct {
			Fields []string `json:"fields"`
			Values [][]any  `json:"values"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode Neo4j Query API response: %w", err)
	}
	records := make([]map[string]any, 0, len(payload.Data.Values))
	for _, row := range payload.Data.Values {
		record := map[string]any{}
		for index, field := range payload.Data.Fields {
			if index < len(row) {
				record[field] = row[index]
			}
		}
		records = append(records, record)
	}
	return snapshotFromRecords(provider, query, records)
}

func (typeDBProvider) Health(ctx context.Context, provider controlentity.WorldProvider) error {
	base := strings.TrimSuffix(strings.TrimRight(provider.Endpoint, "/"), "/v1/query")
	_, err := providerRequest(ctx, provider, http.MethodGet, base+"/health", "", nil)
	return err
}

func (typeDBProvider) Query(ctx context.Context, provider controlentity.WorldProvider, query controlentity.WorldQuery) (*controlentity.WorldSnapshot, []string, error) {
	statement := boundedTemplate(provider.QueryTemplate, query.Limit)
	if !readOnlyTypeQL(statement) {
		return nil, nil, fmt.Errorf("TypeDB provider query must be a read-only match/fetch query")
	}
	body, _ := json.Marshal(map[string]any{"databaseName": provider.Database, "transactionType": "read", "query": statement, "queryOptions": map[string]any{"includeInstanceTypes": true, "answerCountLimit": query.Limit}})
	response, err := providerRequest(ctx, provider, http.MethodPost, typeDBEndpoint(provider), "application/json", body)
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		AnswerType string           `json:"answerType"`
		Answers    []map[string]any `json:"answers"`
		Warning    string           `json:"warning"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, nil, fmt.Errorf("decode TypeDB HTTP response: %w", err)
	}
	records := make([]map[string]any, 0, len(payload.Answers))
	for _, answer := range payload.Answers {
		if data, ok := answer["data"].(map[string]any); ok {
			records = append(records, data)
		} else {
			records = append(records, answer)
		}
	}
	snapshot, warnings, err := snapshotFromRecords(provider, query, records)
	if strings.TrimSpace(payload.Warning) != "" {
		warnings = append(warnings, boundedProviderMessage(payload.Warning))
	}
	return snapshot, warnings, err
}

func providerRequest(ctx context.Context, provider controlentity.WorldProvider, method, target, contentType string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build world provider request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Athena-World-Provider/1")
	if err := applyProviderAuth(request, provider); err != nil {
		return nil, err
	}
	client := safeProviderHTTPClient(provider)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("world provider request failed: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	payload, readErr := io.ReadAll(limited)
	if readErr != nil {
		return nil, fmt.Errorf("read world provider response: %w", readErr)
	}
	if len(payload) > maxProviderResponseBytes {
		return nil, fmt.Errorf("world provider response exceeds %d bytes", maxProviderResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("world provider returned HTTP %d: %s", response.StatusCode, boundedProviderMessage(string(payload)))
	}
	return payload, nil
}

func safeProviderHTTPClient(provider controlentity.WorldProvider) *http.Client {
	dialer := &net.Dialer{Timeout: time.Duration(provider.TimeoutMS) * time.Millisecond, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, ResponseHeaderTimeout: time.Duration(provider.TimeoutMS) * time.Millisecond}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, resolved := range addresses {
			if !provider.AllowPrivateNetwork && privateOrSpecialIP(resolved.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		}
		return nil, fmt.Errorf("world provider endpoint resolves only to blocked private or special-use addresses")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(provider.TimeoutMS) * time.Millisecond,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 || request.URL.User != nil || request.URL.Scheme == "" || request.URL.Host == "" {
				return fmt.Errorf("world provider redirect is not allowed")
			}
			if len(via) > 0 && !strings.EqualFold(request.URL.Hostname(), via[0].URL.Hostname()) {
				return fmt.Errorf("world provider cross-host redirect is not allowed")
			}
			return nil
		},
	}
}

func privateOrSpecialIP(ip net.IP) bool {
	return ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func applyProviderAuth(request *http.Request, provider controlentity.WorldProvider) error {
	if provider.AuthMode == controlentity.WorldProviderAuthNone {
		return nil
	}
	credential, ok := os.LookupEnv(provider.CredentialEnv)
	if !ok || strings.TrimSpace(credential) == "" {
		return fmt.Errorf("world provider credential environment variable %s is not configured", provider.CredentialEnv)
	}
	switch provider.AuthMode {
	case controlentity.WorldProviderAuthBearer:
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential))
	case controlentity.WorldProviderAuthBasic:
		username, password, ok := strings.Cut(credential, ":")
		if !ok || username == "" || password == "" {
			return fmt.Errorf("world provider basic credential must use username:password format")
		}
		request.SetBasicAuth(username, password)
	}
	return nil
}

func neo4jEndpoint(provider controlentity.WorldProvider) string {
	base := strings.TrimRight(provider.Endpoint, "/")
	if strings.HasSuffix(base, "/query/v2") {
		return base
	}
	return base + "/db/" + url.PathEscape(provider.Database) + "/query/v2"
}

func typeDBEndpoint(provider controlentity.WorldProvider) string {
	base := strings.TrimRight(provider.Endpoint, "/")
	if strings.HasSuffix(base, "/v1/query") {
		return base
	}
	return base + "/v1/query"
}

func boundedTemplate(value string, limit int) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "{{limit}}", strconv.Itoa(limit))
}

func readOnlySPARQL(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	return containsQueryKeyword(upper, "SELECT") && !containsQueryKeyword(upper, "INSERT", "DELETE", "LOAD", "CLEAR", "CREATE", "DROP", "COPY", "MOVE", "ADD")
}

func readOnlyCypher(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if !(strings.HasPrefix(upper, "MATCH ") || strings.HasPrefix(upper, "OPTIONAL MATCH ")) || !containsQueryKeyword(upper, "RETURN") {
		return false
	}
	return !containsQueryKeyword(upper, "CREATE", "MERGE", "DELETE", "DETACH", "SET", "REMOVE", "DROP", "LOAD", "CALL", "FOREACH")
}

func readOnlyTypeQL(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(upper, "MATCH ") {
		return false
	}
	return !containsQueryKeyword(upper, "INSERT", "DELETE", "UPDATE", "DEFINE", "REDEFINE", "UNDEFINE", "PUT")
}

func containsQueryKeyword(value string, keywords ...string) bool {
	fields := strings.FieldsFunc(value, func(r rune) bool { return !(r >= 'A' && r <= 'Z') })
	for _, field := range fields {
		for _, keyword := range keywords {
			if field == keyword {
				return true
			}
		}
	}
	return false
}

func unwrapAPIData(payload []byte) []byte {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(payload, &envelope) == nil && len(envelope["data"]) > 0 {
		return envelope["data"]
	}
	return payload
}

func snapshotFromRecords(provider controlentity.WorldProvider, query controlentity.WorldQuery, records []map[string]any) (*controlentity.WorldSnapshot, []string, error) {
	now := query.AsOf
	if now.IsZero() {
		now = time.Now().UTC()
	}
	snapshot := &controlentity.WorldSnapshot{Schema: SnapshotSchema, OntologyPack: provider.OntologyPack, OntologyVersion: provider.OntologyVersion, CapturedAt: now}
	warnings := make([]string, 0)
	for index, record := range records {
		converted, warning := recordsFromExternalValue(provider, record, index, now)
		snapshot.Entities = append(snapshot.Entities, converted.Entities...)
		snapshot.Relations = append(snapshot.Relations, converted.Relations...)
		snapshot.Facts = append(snapshot.Facts, converted.Facts...)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	decorateSnapshot(snapshot, provider, now)
	filterProviderSnapshot(snapshot, query)
	return snapshot, warnings, nil
}

func recordsFromExternalValue(provider controlentity.WorldProvider, record map[string]any, index int, now time.Time) (controlentity.WorldSnapshot, string) {
	if value, ok := record["athena"].(map[string]any); ok {
		record = value
	}
	result := controlentity.WorldSnapshot{}
	kind := strings.ToLower(firstAnyString(record, "athena_kind", "kind"))
	if kind != "" {
		appendExplicitWorldRecord(&result, provider, record, kind, index, now)
		return result, ""
	}
	var firstEntityID, firstEntityType string
	for _, raw := range record {
		value, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conceptKind := strings.ToLower(firstAnyString(value, "kind"))
		if value["elementId"] != nil || conceptKind == "entity" {
			id := firstAnyString(value, "elementId", "iid", "id")
			typeName := nestedTypeLabel(value)
			if typeName == "" {
				typeName = firstStringSlice(value["labels"])
			}
			properties, _ := value["properties"].(map[string]any)
			name := firstAnyString(properties, "canonical_name", "name", "title")
			result.Entities = append(result.Entities, externalEntity(provider, id, typeName, name, properties, now))
			if firstEntityID == "" {
				firstEntityID, firstEntityType = id, typeName
			}
			continue
		}
		if conceptKind == "relation" || value["startNodeElementId"] != nil {
			id := firstAnyString(value, "elementId", "iid", "id")
			source, target := firstAnyString(value, "startNodeElementId", "source_id", "source"), firstAnyString(value, "endNodeElementId", "target_id", "target")
			if source != "" && target != "" {
				result.Relations = append(result.Relations, externalRelation(provider, id, source, target, firstAnyString(value, "type", "predicate"), value, now))
			}
		}
	}
	for _, raw := range record {
		value, ok := raw.(map[string]any)
		if !ok || strings.ToLower(firstAnyString(value, "kind")) != "attribute" || firstEntityID == "" {
			continue
		}
		predicate := nestedTypeLabel(value)
		factID := stableExternalID("fact", provider.ProviderID, firstEntityID, predicate, strconv.Itoa(index))
		result.Facts = append(result.Facts, externalFact(provider, factID, firstEntityID, firstEntityType, predicate, value["value"], firstAnyString(value, "valueType"), now))
	}
	if len(result.Entities)+len(result.Relations)+len(result.Facts) == 0 {
		return result, fmt.Sprintf("record %d did not expose Athena aliases or a supported graph value", index+1)
	}
	return result, ""
}

func appendExplicitWorldRecord(result *controlentity.WorldSnapshot, provider controlentity.WorldProvider, record map[string]any, kind string, index int, now time.Time) {
	switch kind {
	case "entity":
		id := firstAnyString(record, "entity_id", "id", "iri")
		if id == "" {
			id = stableExternalID("entity", provider.ProviderID, strconv.Itoa(index))
		}
		result.Entities = append(result.Entities, externalEntity(provider, id, firstAnyString(record, "entity_type", "type"), firstAnyString(record, "canonical_name", "name", "label"), record, now))
	case "relation":
		id := firstAnyString(record, "relation_id", "id")
		if id == "" {
			id = stableExternalID("relation", provider.ProviderID, strconv.Itoa(index))
		}
		result.Relations = append(result.Relations, externalRelation(provider, id, firstAnyString(record, "source_id", "source"), firstAnyString(record, "target_id", "target"), firstAnyString(record, "predicate", "relation_type", "type"), record, now))
	case "fact":
		id := firstAnyString(record, "fact_id", "id")
		if id == "" {
			id = stableExternalID("fact", provider.ProviderID, strconv.Itoa(index))
		}
		result.Facts = append(result.Facts, externalFact(provider, id, firstAnyString(record, "subject_id", "subject"), firstAnyString(record, "subject_type"), firstAnyString(record, "predicate"), record["value"], firstAnyString(record, "value_type"), now))
	}
}

func externalEntity(provider controlentity.WorldProvider, id, kind, name string, properties map[string]any, now time.Time) controlentity.WorldEntity {
	return controlentity.WorldEntity{EntityID: id, Scope: "provider:" + provider.ProviderID, Type: kind, CanonicalName: name, Properties: properties, Evidence: []controlentity.EvidenceRef{providerEvidence(provider, id)}, Confidence: provider.DefaultConfidence, ObservedAt: now, ExpiresAt: now.Add(time.Duration(provider.TTLSeconds) * time.Second), Revision: 1}
}

func externalRelation(provider controlentity.WorldProvider, id, source, target, predicate string, properties map[string]any, now time.Time) controlentity.WorldRelation {
	return controlentity.WorldRelation{RelationID: id, Scope: "provider:" + provider.ProviderID, SourceID: source, TargetID: target, Predicate: predicate, Properties: properties, Evidence: []controlentity.EvidenceRef{providerEvidence(provider, id)}, Confidence: provider.DefaultConfidence, ObservedAt: now, ExpiresAt: now.Add(time.Duration(provider.TTLSeconds) * time.Second), Revision: 1}
}

func externalFact(provider controlentity.WorldProvider, id, subjectID, subjectType, predicate string, value any, valueType string, now time.Time) controlentity.WorldFact {
	return controlentity.WorldFact{FactID: id, Scope: "provider:" + provider.ProviderID, SubjectID: subjectID, SubjectType: subjectType, Predicate: predicate, Value: value, ValueType: valueType, Evidence: []controlentity.EvidenceRef{providerEvidence(provider, id)}, Confidence: provider.DefaultConfidence, ObservedAt: now, ExpiresAt: now.Add(time.Duration(provider.TTLSeconds) * time.Second), Revision: 1}
}

func providerEvidence(provider controlentity.WorldProvider, objectID string) controlentity.EvidenceRef {
	return controlentity.EvidenceRef{EvidenceID: stableExternalID("provider-evidence", provider.ProviderID, objectID), Kind: "world_provider", URI: provider.Endpoint, Summary: "Read-only external world provider projection", Metadata: map[string]any{"provider_id": provider.ProviderID, "provider_kind": provider.Kind}}
}

func decorateSnapshot(snapshot *controlentity.WorldSnapshot, provider controlentity.WorldProvider, now time.Time) {
	snapshot.OntologyPack, snapshot.OntologyVersion = provider.OntologyPack, provider.OntologyVersion
	for index := range snapshot.Entities {
		decorateEntity(&snapshot.Entities[index], provider, now)
	}
	for index := range snapshot.Relations {
		decorateRelation(&snapshot.Relations[index], provider, now)
	}
	for index := range snapshot.Facts {
		decorateFact(&snapshot.Facts[index], provider, now)
	}
}

func decorateEntity(value *controlentity.WorldEntity, provider controlentity.WorldProvider, now time.Time) {
	value.Scope, value.Confidence, value.ObservedAt, value.ExpiresAt, value.Revision = "provider:"+provider.ProviderID, boundedConfidence(value.Confidence, provider.DefaultConfidence), now, now.Add(time.Duration(provider.TTLSeconds)*time.Second), 1
	value.Evidence = appendProviderEvidence(value.Evidence, providerEvidence(provider, value.EntityID))
}
func decorateRelation(value *controlentity.WorldRelation, provider controlentity.WorldProvider, now time.Time) {
	value.Scope, value.Confidence, value.ObservedAt, value.ExpiresAt, value.Revision = "provider:"+provider.ProviderID, boundedConfidence(value.Confidence, provider.DefaultConfidence), now, now.Add(time.Duration(provider.TTLSeconds)*time.Second), 1
	value.Evidence = appendProviderEvidence(value.Evidence, providerEvidence(provider, value.RelationID))
}
func decorateFact(value *controlentity.WorldFact, provider controlentity.WorldProvider, now time.Time) {
	value.Scope, value.Confidence, value.ObservedAt, value.ExpiresAt, value.Revision = "provider:"+provider.ProviderID, boundedConfidence(value.Confidence, provider.DefaultConfidence), now, now.Add(time.Duration(provider.TTLSeconds)*time.Second), 1
	value.Evidence = appendProviderEvidence(value.Evidence, providerEvidence(provider, value.FactID))
}

func appendProviderEvidence(values []controlentity.EvidenceRef, evidence controlentity.EvidenceRef) []controlentity.EvidenceRef {
	for _, value := range values {
		if value.EvidenceID == evidence.EvidenceID {
			return values
		}
	}
	return append(values, evidence)
}

func boundedConfidence(value, ceiling float64) float64 {
	if value <= 0 || value > ceiling {
		return ceiling
	}
	return value
}

func filterProviderSnapshot(snapshot *controlentity.WorldSnapshot, query controlentity.WorldQuery) {
	text := strings.ToLower(strings.TrimSpace(query.Text))
	entityTypes, predicates := stringSet(query.EntityTypes), stringSet(query.Predicates)
	entities := snapshot.Entities[:0]
	for _, value := range snapshot.Entities {
		if len(entityTypes) > 0 && !entityTypes[value.Type] {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(value.EntityID+" "+value.CanonicalName), text) {
			continue
		}
		entities = append(entities, value)
		if len(entities) >= query.Limit {
			break
		}
	}
	snapshot.Entities = entities
	relations := snapshot.Relations[:0]
	for _, value := range snapshot.Relations {
		if len(predicates) > 0 && !predicates[value.Predicate] {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(value.RelationID+" "+value.Predicate+" "+value.SourceID+" "+value.TargetID), text) {
			continue
		}
		relations = append(relations, value)
		if len(relations) >= query.Limit {
			break
		}
	}
	snapshot.Relations = relations
	facts := snapshot.Facts[:0]
	for _, value := range snapshot.Facts {
		if len(predicates) > 0 && !predicates[value.Predicate] {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(fmt.Sprint(value.FactID, " ", value.SubjectID, " ", value.Predicate, " ", value.Value)), text) {
			continue
		}
		facts = append(facts, value)
		if len(facts) >= query.Limit {
			break
		}
	}
	snapshot.Facts = facts
}

func firstAnyString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if nested, ok := value[key].(map[string]any); ok {
			if text := firstAnyString(nested, "value", "label", "iid", "elementId", "id"); text != "" {
				return text
			}
		}
	}
	return ""
}

func nestedTypeLabel(value map[string]any) string {
	if typed, ok := value["type"].(map[string]any); ok {
		return firstAnyString(typed, "label", "name")
	}
	return firstAnyString(value, "type", "label")
}

func firstStringSlice(value any) string {
	if values, ok := value.([]any); ok && len(values) > 0 {
		text, _ := values[0].(string)
		return text
	}
	if values, ok := value.([]string); ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

func stableExternalID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "-" + hex.EncodeToString(sum[:12])
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
