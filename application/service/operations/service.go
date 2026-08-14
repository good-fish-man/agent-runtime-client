// Package operations aggregates production health and SLO evidence without
// exposing database credentials, device tokens, or internal network details.
package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

const requestTimeout = 3 * time.Second

type DeviceSource interface {
	Devices(context.Context, string) ([]controlsvc.Device, error)
}

type DatabaseProbe func(context.Context) error

// GAEvidenceStore persists append-only golden-journey suites. Implementations
// must scope every read and write by owner ID and verify stored content hashes.
type GAEvidenceStore interface {
	SaveGoldenJourneyResults(context.Context, string, []ga.GoldenJourneyResult) error
	LastGoldenJourneyResults(context.Context, string, string) ([]ga.GoldenJourneyResult, error)
}

type Service struct {
	runtimeURL string
	database   DatabaseProbe
	devices    DeviceSource
	client     *http.Client
	startedAt  time.Time
	instanceID string
	backup     *BackupManager
	gaConfig   GAConfig
	gaStore    GAEvidenceStore
	gaMu       sync.RWMutex
	gaRuns     map[string][][]ga.GoldenJourneyResult
}

func (s *Service) WithBackupManager(manager *BackupManager) *Service {
	s.backup = manager
	return s
}

func (s *Service) BackupManager() *BackupManager { return s.backup }

type Snapshot struct {
	Schema          string                       `json:"schema"`
	Health          operationsv1.HealthSnapshot  `json:"health"`
	RuntimeHealth   *operationsv1.HealthSnapshot `json:"runtime_health,omitempty"`
	SLO             *operationsv1.SLOSnapshot    `json:"slo,omitempty"`
	OnlineDevices   int                          `json:"online_devices"`
	TotalDevices    int                          `json:"total_devices"`
	RecoveryManaged bool                         `json:"recovery_managed"`
	ObservedAt      time.Time                    `json:"observed_at"`
}

func New(runtimeURL string, database DatabaseProbe, devices DeviceSource) *Service {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return &Service{
		runtimeURL: strings.TrimRight(strings.TrimSpace(runtimeURL), "/"), database: database, devices: devices,
		client: &http.Client{Timeout: requestTimeout}, startedAt: time.Now().UTC(),
		instanceID: fmt.Sprintf("%s-%d", host, os.Getpid()),
		gaRuns:     make(map[string][][]ga.GoldenJourneyResult),
	}
}

func (s *Service) WithGAEvidenceStore(store GAEvidenceStore) *Service {
	s.gaStore = store
	return s
}

func (s *Service) WithHTTPClient(client *http.Client) *Service {
	if client != nil {
		s.client = client
	}
	return s
}

func (s *Service) Snapshot(ctx context.Context, userID string) Snapshot {
	now := time.Now().UTC()
	checks := make([]operationsv1.HealthCheck, 0, 3)
	status := operationsv1.HealthHealthy

	runtimeHealth, runtimeSLO, runtimeLatency, runtimeErr := s.runtimeSnapshot(ctx)
	if runtimeErr != nil {
		status = operationsv1.HealthUnhealthy
		checks = append(checks, healthCheck("runtime", operationsv1.HealthUnhealthy, runtimeLatency, runtimeErr.Error()))
	} else {
		checks = append(checks, healthCheck("runtime", runtimeHealth.Status, runtimeLatency, ""))
		status = worstHealth(status, runtimeHealth.Status)
	}

	if s.database != nil {
		started := time.Now()
		probeCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		err := s.database(probeCtx)
		cancel()
		if err != nil {
			status = operationsv1.HealthUnhealthy
			checks = append(checks, healthCheck("database", operationsv1.HealthUnhealthy, time.Since(started), "database probe failed"))
		} else {
			checks = append(checks, healthCheck("database", operationsv1.HealthHealthy, time.Since(started), ""))
		}
	}

	var totalDevices, onlineDevices int
	if s.devices != nil {
		devices, err := s.devices.Devices(ctx, userID)
		if err != nil {
			status = worstHealth(status, operationsv1.HealthDegraded)
			checks = append(checks, healthCheck("device-control", operationsv1.HealthDegraded, 0, "device inventory unavailable"))
		} else {
			totalDevices = len(devices)
			for _, device := range devices {
				if device.Online && (device.LeaseExpiresAt.IsZero() || device.LeaseExpiresAt.After(now)) {
					onlineDevices++
				}
			}
			deviceStatus, message := operationsv1.HealthHealthy, ""
			if onlineDevices == 0 {
				deviceStatus, message = operationsv1.HealthDegraded, "no online device for this account"
				status = worstHealth(status, deviceStatus)
			}
			checks = append(checks, healthCheck("device-control", deviceStatus, 0, message))
		}
	}

	return Snapshot{
		Schema: operationsv1.Schema,
		Health: operationsv1.HealthSnapshot{
			Schema: operationsv1.Schema, Component: consts.ServiceName, Version: consts.Version,
			InstanceID: s.instanceID, Status: status, UptimeMS: time.Since(s.startedAt).Milliseconds(),
			Checks: checks, ObservedAt: now,
		},
		RuntimeHealth: runtimeHealth, SLO: runtimeSLO, OnlineDevices: onlineDevices,
		TotalDevices: totalDevices, RecoveryManaged: s.backup != nil, ObservedAt: now,
	}
}

func (s *Service) runtimeSnapshot(ctx context.Context) (*operationsv1.HealthSnapshot, *operationsv1.SLOSnapshot, time.Duration, error) {
	if s.runtimeURL == "" {
		return nil, nil, 0, fmt.Errorf("runtime endpoint is not configured")
	}
	endpoint, err := url.JoinPath(s.runtimeURL, "metrics")
	if err != nil {
		return nil, nil, 0, fmt.Errorf("build runtime metrics URL: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("create runtime metrics request: %w", err)
	}
	started := time.Now()
	response, err := s.client.Do(request)
	latency := time.Since(started)
	if err != nil {
		return nil, nil, latency, fmt.Errorf("runtime metrics request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, latency, fmt.Errorf("runtime metrics returned status %d", response.StatusCode)
	}
	var payload struct {
		Health operationsv1.HealthSnapshot `json:"health"`
		SLO    operationsv1.SLOSnapshot    `json:"slo"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, latency, fmt.Errorf("decode runtime metrics: %w", err)
	}
	if err := payload.Health.Validate(); err != nil {
		return nil, nil, latency, fmt.Errorf("validate runtime health: %w", err)
	}
	if err := payload.SLO.Validate(); err != nil {
		return nil, nil, latency, fmt.Errorf("validate runtime SLO: %w", err)
	}
	return &payload.Health, &payload.SLO, latency, nil
}

func healthCheck(name, status string, latency time.Duration, message string) operationsv1.HealthCheck {
	return operationsv1.HealthCheck{Name: name, Status: status, LatencyMS: latency.Milliseconds(), Message: message}
}

func worstHealth(left, right string) string {
	rank := map[string]int{operationsv1.HealthHealthy: 0, operationsv1.HealthDegraded: 1, operationsv1.HealthUnhealthy: 2}
	if rank[right] > rank[left] {
		return right
	}
	return left
}
