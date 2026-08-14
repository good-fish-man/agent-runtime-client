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
	Plugins       PluginsConfig       `yaml:"plugins"`
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
		Plugins: PluginsConfig{
			Directory: filepath.Join(pluginRoot, "packages"), RegistryPath: filepath.Join(pluginRoot, "registry.json"),
			TrustStorePath: filepath.Join(pluginRoot, "trust-store.json"), AuditPath: filepath.Join(pluginRoot, "logs", "invocations.jsonl"),
		},
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
}
