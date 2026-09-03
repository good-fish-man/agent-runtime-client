// Package config loads agent-runtime-client configuration from a YAML file with
// environment-variable overrides. It intentionally avoids third-party config
// frameworks to keep the client self-contained.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// Config is the root configuration.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Runtime       RuntimeConfig       `yaml:"runtime"`
	Log           LogConfig           `yaml:"log"`
	Model         ModelConfig         `yaml:"model"`
	DB            DBConfig            `yaml:"db"`
	Paths         PathsConfig         `yaml:"paths"`
	Control       ControlConfig       `yaml:"control"`
	ScheduledTask ScheduledTaskConfig `yaml:"scheduled_task"`
	Orchestration OrchestrationConfig `yaml:"orchestration"`
	Evolution     EvolutionConfig     `yaml:"evolution"`
	Plugins       PluginsConfig       `yaml:"plugins"`
	Operations    OperationsConfig    `yaml:"operations"`
}

type OperationsConfig struct {
	BackupDir         string `yaml:"backup_dir"`
	EncryptionKeyFile string `yaml:"encryption_key_file"`
	PGDumpPath        string `yaml:"pg_dump_path"`
	PGRestorePath     string `yaml:"pg_restore_path"`
	MaxBackups        int    `yaml:"max_backups"`
}

type PluginsConfig struct {
	Directory      string `yaml:"directory"`
	RegistryPath   string `yaml:"registry_path"`
	TrustStorePath string `yaml:"trust_store_path"`
	AuditPath      string `yaml:"audit_path"`
}

type ControlConfig struct {
	DeviceToken string `yaml:"device_token"`
}

type ScheduledTaskConfig struct {
	ScanIntervalSec int `yaml:"scan_interval_sec"`
}

type OrchestrationConfig struct {
	Enabled           bool `yaml:"enabled"`
	ScanIntervalSec   int  `yaml:"scan_interval_sec"`
	MaxConcurrentRuns int  `yaml:"max_concurrent_runs"`
}

// EvolutionConfig controls evidence discovery and optional constrained Codex
// synthesis. It never enables automatic review, promotion, deployment, or
// executable-code generation.
type EvolutionConfig struct {
	Enabled                 bool                 `yaml:"enabled"`
	ScanIntervalSec         int                  `yaml:"scan_interval_sec"`
	OwnerBatchSize          int                  `yaml:"owner_batch_size"`
	ExperienceLimit         int                  `yaml:"experience_limit"`
	MaxCandidatesPerScan    int                  `yaml:"max_candidates_per_scan"`
	MinimumNovelExperiences int                  `yaml:"minimum_novel_experiences"`
	Codex                   EvolutionCodexConfig `yaml:"codex"`
}

// EvolutionCodexConfig is an opt-in platform model used only to propose
// declarative candidates from bounded, sanitized evidence.
type EvolutionCodexConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Model           string `yaml:"model"`
	APIKey          string `yaml:"api_key"`
	APIBase         string `yaml:"api_base"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	TimeoutSec      int    `yaml:"timeout_sec"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
}

// DBConfig configures the shared relational database. Field names mirror
// agent-frame's db section so both services can point at the same instance.
type DBConfig struct {
	DBType          string `yaml:"db_type"` // postgres | mysql
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	DBHost          string `yaml:"db_host"`
	DBPort          int    `yaml:"db_port"`
	DBName          string `yaml:"db_name"`
	Charset         string `yaml:"charset"`
	MaxOpenConn     int    `yaml:"max_open_conn"`
	MaxIdleConn     int    `yaml:"max_idle_conn"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // seconds
	LogMode         int    `yaml:"log_mode"`          // gorm logger level (1=silent..4=info)
	SlowThreshold   int    `yaml:"slow_threshold"`    // slow query threshold, ms
}

// Enabled reports whether a database connection should be established.
func (d DBConfig) Enabled() bool {
	return strings.TrimSpace(d.DBHost) != "" && strings.TrimSpace(d.DBName) != ""
}

// PathsConfig configures local filesystem locations used by the file-backed
// endpoints (config YAML read/write, uploads, HTML reports).
type PathsConfig struct {
	AppConfigFile    string `yaml:"app_config_file"`    // config/app read/write target
	SkillsConfigFile string `yaml:"skills_config_file"` // config/skills read/write target
	UploadsDir       string `yaml:"uploads_dir"`        // runner uploads + reports root
}

// ServerConfig configures the inbound HTTP (Gin) server.
type ServerConfig struct {
	Name         string `yaml:"name"`
	HTTPAddr     string `yaml:"http_addr"`
	Mode         string `yaml:"mode"`          // gin mode: debug | release | test
	PublicPrefix string `yaml:"public_prefix"` // mount prefix for agent-frame-compatible public routes
}

// RuntimeConfig configures the outbound gRPC connection to agent-runtime.
type RuntimeConfig struct {
	GRPCAddr          string `yaml:"grpc_addr"`
	HTTPAddr          string `yaml:"http_addr"`
	RequestTimeoutSec int    `yaml:"request_timeout_sec"`
	DialTimeoutSec    int    `yaml:"dial_timeout_sec"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level string `yaml:"level"` // debug | info | warn | error
}

// ModelConfig is the fallback model applied when a request omits one.
type ModelConfig struct {
	Provider string `yaml:"provider"`
	Name     string `yaml:"name"`
	APIKey   string `yaml:"api_key"`
	APIBase  string `yaml:"api_base"`
}

// Default returns a Config with sane defaults.
func Default() *Config {
	pluginRoot := defaultPluginRoot()
	return &Config{
		Server: ServerConfig{
			Name:         consts.ServiceName,
			HTTPAddr:     consts.DefaultHTTPAddr,
			Mode:         consts.DefaultGinMode,
			PublicPrefix: consts.DefaultPublicPrefix,
		},
		Runtime: RuntimeConfig{
			GRPCAddr:          consts.DefaultRuntimeGRPCAddr,
			HTTPAddr:          consts.DefaultRuntimeHTTPAddr,
			RequestTimeoutSec: consts.DefaultRequestTimeoutSec,
			DialTimeoutSec:    consts.DefaultDialTimeoutSec,
		},
		Log: LogConfig{Level: "debug"},
		DB: DBConfig{
			DBType:          "postgres",
			MaxOpenConn:     50,
			MaxIdleConn:     10,
			ConnMaxLifetime: 500,
			LogMode:         4,
			SlowThreshold:   10,
			Charset:         "utf8mb4",
		},
		ScheduledTask: ScheduledTaskConfig{ScanIntervalSec: consts.DefaultScheduledTaskScanIntervalSec},
		Orchestration: OrchestrationConfig{Enabled: true, ScanIntervalSec: consts.DefaultOrchestrationScanIntervalSec, MaxConcurrentRuns: consts.DefaultOrchestrationConcurrency},
		Evolution: EvolutionConfig{
			Enabled: true, ScanIntervalSec: consts.DefaultEvolutionScanIntervalSec,
			OwnerBatchSize: consts.DefaultEvolutionOwnerBatchSize, ExperienceLimit: consts.DefaultEvolutionExperienceLimit,
			MaxCandidatesPerScan: consts.DefaultEvolutionCandidatesPerScan, MinimumNovelExperiences: consts.DefaultEvolutionMinimumNovel,
			Codex: EvolutionCodexConfig{
				Enabled: false, Model: consts.DefaultEvolutionCodexModel, APIKey: "${OPENAI_API_KEY}",
				APIBase: consts.DefaultEvolutionCodexAPIBase, ReasoningEffort: "medium",
				TimeoutSec: consts.DefaultEvolutionCodexTimeoutSec, MaxOutputTokens: consts.DefaultEvolutionCodexMaxOutputTokens,
			},
		},
		Plugins: PluginsConfig{
			Directory: filepath.Join(pluginRoot, "packages"), RegistryPath: filepath.Join(pluginRoot, "registry.json"),
			TrustStorePath: filepath.Join(pluginRoot, "trust-store.json"), AuditPath: filepath.Join(pluginRoot, "logs", "invocations.jsonl"),
		},
		Operations: OperationsConfig{PGDumpPath: "pg_dump", PGRestorePath: "pg_restore", MaxBackups: 10},
	}
}

func defaultPluginRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Clean(".athena/plugins")
	}
	return filepath.Join(home, ".athena", "plugins")
}

// Load reads config from path (falling back to $ARC_CONFIG_PATH, then defaults)
// and applies environment overrides. A missing file is not an error: defaults
// plus env overrides are used.
func Load(path string) (*Config, error) {
	cfg := Default()
	path = ResolvePath(path)

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// ResolvePath returns the absolute configuration path selected by arguments or environment.
func ResolvePath(path string) string {
	if path == "" {
		path = os.Getenv(consts.EnvConfigPath)
	}
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ATHENA_PLUGINS_DIR"); v != "" {
		cfg.Plugins.Directory = v
	}
	if v := os.Getenv("ATHENA_PLUGIN_REGISTRY_PATH"); v != "" {
		cfg.Plugins.RegistryPath = v
	}
	if v := os.Getenv("ATHENA_PLUGIN_TRUST_STORE_PATH"); v != "" {
		cfg.Plugins.TrustStorePath = v
	}
	if v := os.Getenv("ATHENA_PLUGIN_AUDIT_PATH"); v != "" {
		cfg.Plugins.AuditPath = v
	}
	if v := os.Getenv("ATHENA_BACKUP_DIR"); v != "" {
		cfg.Operations.BackupDir = v
	}
	if v := os.Getenv("ATHENA_BACKUP_KEY_FILE"); v != "" {
		cfg.Operations.EncryptionKeyFile = v
	}
	if v := os.Getenv("ATHENA_PG_DUMP_PATH"); v != "" {
		cfg.Operations.PGDumpPath = v
	}
	if v := os.Getenv("ATHENA_PG_RESTORE_PATH"); v != "" {
		cfg.Operations.PGRestorePath = v
	}
	if v := os.Getenv("ATHENA_DEVICE_TOKEN"); v != "" {
		cfg.Control.DeviceToken = v
	}
	if v := os.Getenv(consts.EnvHTTPAddr); v != "" {
		cfg.Server.HTTPAddr = v
	}
	if v := os.Getenv(consts.EnvGinMode); v != "" {
		cfg.Server.Mode = v
	}
	if v := os.Getenv(consts.EnvPublicPrefix); v != "" {
		cfg.Server.PublicPrefix = v
	}
	if v := os.Getenv(consts.EnvRuntimeGRPCAddr); v != "" {
		cfg.Runtime.GRPCAddr = v
	}
	if v := os.Getenv(consts.EnvRuntimeHTTPAddr); v != "" {
		cfg.Runtime.HTTPAddr = v
	}
	if v := os.Getenv(consts.EnvDefaultProvider); v != "" {
		cfg.Model.Provider = v
	}
	if v := os.Getenv(consts.EnvDefaultModel); v != "" {
		cfg.Model.Name = v
	}
	if v := os.Getenv(consts.EnvDefaultAPIKey); v != "" {
		cfg.Model.APIKey = v
	}
	if v := os.Getenv(consts.EnvDefaultAPIBase); v != "" {
		cfg.Model.APIBase = v
	}
	if v := os.Getenv(consts.EnvScheduledTaskScanIntervalSec); v != "" {
		if sec, err := strconv.Atoi(v); err == nil {
			cfg.ScheduledTask.ScanIntervalSec = sec
		}
	}
	if v := os.Getenv(consts.EnvOrchestrationEnabled); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Orchestration.Enabled = enabled
		}
	}
	if v := os.Getenv(consts.EnvOrchestrationScanIntervalSec); v != "" {
		if sec, err := strconv.Atoi(v); err == nil {
			cfg.Orchestration.ScanIntervalSec = sec
		}
	}
	if v := os.Getenv(consts.EnvOrchestrationConcurrency); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			cfg.Orchestration.MaxConcurrentRuns = count
		}
	}
	if v := os.Getenv(consts.EnvEvolutionEnabled); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Evolution.Enabled = enabled
		}
	}
	if v := os.Getenv(consts.EnvEvolutionScanIntervalSec); v != "" {
		if sec, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.ScanIntervalSec = sec
		}
	}
	if v := os.Getenv(consts.EnvEvolutionOwnerBatchSize); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.OwnerBatchSize = count
		}
	}
	if v := os.Getenv(consts.EnvEvolutionExperienceLimit); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.ExperienceLimit = count
		}
	}
	if v := os.Getenv(consts.EnvEvolutionCandidatesPerScan); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.MaxCandidatesPerScan = count
		}
	}
	if v := os.Getenv(consts.EnvEvolutionMinimumNovel); v != "" {
		if count, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.MinimumNovelExperiences = count
		}
	}
	if v := os.Getenv(consts.EnvEvolutionCodexEnabled); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.Evolution.Codex.Enabled = enabled
		}
	}
	if v := os.Getenv(consts.EnvEvolutionCodexModel); v != "" {
		cfg.Evolution.Codex.Model = v
	}
	if v := os.Getenv(consts.EnvEvolutionCodexAPIKey); v != "" {
		cfg.Evolution.Codex.APIKey = v
	}
	if v := os.Getenv(consts.EnvEvolutionCodexAPIBase); v != "" {
		cfg.Evolution.Codex.APIBase = v
	}
	if v := os.Getenv(consts.EnvEvolutionCodexReasoning); v != "" {
		cfg.Evolution.Codex.ReasoningEffort = v
	}
	if v := os.Getenv(consts.EnvEvolutionCodexTimeoutSec); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.Codex.TimeoutSec = seconds
		}
	}
	if v := os.Getenv(consts.EnvEvolutionCodexMaxOutputTokens); v != "" {
		if tokens, err := strconv.Atoi(v); err == nil {
			cfg.Evolution.Codex.MaxOutputTokens = tokens
		}
	}

	// Database overrides (shared with agent-frame).
	if v := os.Getenv(consts.EnvDBType); v != "" {
		cfg.DB.DBType = v
	}
	if v := os.Getenv(consts.EnvDBHost); v != "" {
		cfg.DB.DBHost = v
	}
	if v := os.Getenv(consts.EnvDBPort); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.DB.DBPort = p
		}
	}
	if v := os.Getenv(consts.EnvDBUser); v != "" {
		cfg.DB.Username = v
	}
	if v := os.Getenv(consts.EnvDBPassword); v != "" {
		cfg.DB.Password = v
	}
	if v := os.Getenv(consts.EnvDBName); v != "" {
		cfg.DB.DBName = v
	}

	// Guardrails.
	if cfg.Runtime.RequestTimeoutSec <= 0 {
		cfg.Runtime.RequestTimeoutSec = consts.DefaultRequestTimeoutSec
	}
	if cfg.Runtime.DialTimeoutSec <= 0 {
		cfg.Runtime.DialTimeoutSec = consts.DefaultDialTimeoutSec
	}
	if cfg.Server.HTTPAddr == "" {
		cfg.Server.HTTPAddr = consts.DefaultHTTPAddr
	}
	if strings.TrimSpace(cfg.Server.PublicPrefix) == "" {
		cfg.Server.PublicPrefix = consts.DefaultPublicPrefix
	}
	if cfg.Runtime.GRPCAddr == "" {
		cfg.Runtime.GRPCAddr = consts.DefaultRuntimeGRPCAddr
	}
	if cfg.Runtime.HTTPAddr == "" {
		cfg.Runtime.HTTPAddr = consts.DefaultRuntimeHTTPAddr
	}
	if cfg.ScheduledTask.ScanIntervalSec <= 0 {
		cfg.ScheduledTask.ScanIntervalSec = consts.DefaultScheduledTaskScanIntervalSec
	}
	if cfg.Orchestration.ScanIntervalSec <= 0 {
		cfg.Orchestration.ScanIntervalSec = consts.DefaultOrchestrationScanIntervalSec
	}
	if cfg.Orchestration.MaxConcurrentRuns <= 0 {
		cfg.Orchestration.MaxConcurrentRuns = consts.DefaultOrchestrationConcurrency
	}
	if cfg.Evolution.ScanIntervalSec <= 0 {
		cfg.Evolution.ScanIntervalSec = consts.DefaultEvolutionScanIntervalSec
	}
	if cfg.Evolution.OwnerBatchSize <= 0 || cfg.Evolution.OwnerBatchSize > 200 {
		cfg.Evolution.OwnerBatchSize = consts.DefaultEvolutionOwnerBatchSize
	}
	if cfg.Evolution.ExperienceLimit <= 0 || cfg.Evolution.ExperienceLimit > 2000 {
		cfg.Evolution.ExperienceLimit = consts.DefaultEvolutionExperienceLimit
	}
	if cfg.Evolution.MaxCandidatesPerScan <= 0 || cfg.Evolution.MaxCandidatesPerScan > 100 {
		cfg.Evolution.MaxCandidatesPerScan = consts.DefaultEvolutionCandidatesPerScan
	}
	if cfg.Evolution.MinimumNovelExperiences <= 0 {
		cfg.Evolution.MinimumNovelExperiences = consts.DefaultEvolutionMinimumNovel
	}
	if strings.TrimSpace(cfg.Evolution.Codex.Model) == "" {
		cfg.Evolution.Codex.Model = consts.DefaultEvolutionCodexModel
	}
	if strings.TrimSpace(cfg.Evolution.Codex.APIBase) == "" {
		cfg.Evolution.Codex.APIBase = consts.DefaultEvolutionCodexAPIBase
	}
	if strings.TrimSpace(cfg.Evolution.Codex.ReasoningEffort) == "" {
		cfg.Evolution.Codex.ReasoningEffort = "medium"
	}
	if cfg.Evolution.Codex.TimeoutSec <= 0 {
		cfg.Evolution.Codex.TimeoutSec = consts.DefaultEvolutionCodexTimeoutSec
	}
	if cfg.Evolution.Codex.MaxOutputTokens < 512 || cfg.Evolution.Codex.MaxOutputTokens > 32768 {
		cfg.Evolution.Codex.MaxOutputTokens = consts.DefaultEvolutionCodexMaxOutputTokens
	}
	defaults := Default().Plugins
	if strings.TrimSpace(cfg.Plugins.Directory) == "" {
		cfg.Plugins.Directory = defaults.Directory
	}
	if strings.TrimSpace(cfg.Plugins.RegistryPath) == "" {
		cfg.Plugins.RegistryPath = defaults.RegistryPath
	}
	if strings.TrimSpace(cfg.Plugins.TrustStorePath) == "" {
		cfg.Plugins.TrustStorePath = defaults.TrustStorePath
	}
	if strings.TrimSpace(cfg.Plugins.AuditPath) == "" {
		cfg.Plugins.AuditPath = defaults.AuditPath
	}
	if strings.TrimSpace(cfg.Operations.PGDumpPath) == "" {
		cfg.Operations.PGDumpPath = "pg_dump"
	}
	if strings.TrimSpace(cfg.Operations.PGRestorePath) == "" {
		cfg.Operations.PGRestorePath = "pg_restore"
	}
	if cfg.Operations.MaxBackups <= 0 {
		cfg.Operations.MaxBackups = 10
	}
}

// BootstrapAdministrator reads installation bootstrap material on demand so a
// long-lived Config value can never expose the plaintext password through
// serialization or debug formatting.
func BootstrapAdministrator() (username, password string) {
	username = strings.TrimSpace(os.Getenv(consts.EnvBootstrapAdminUsername))
	if username == "" {
		username = "athena"
	}
	return username, os.Getenv(consts.EnvBootstrapAdminPassword)
}
