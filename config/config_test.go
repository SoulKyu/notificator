package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// NOTIFICATOR_GRPC_REFLECTION was bound in setViperDefaults but never reached
// the struct: viper.Unmarshal compares the lowercased field name
// ("enablereflection") against the key ("enable_reflection") when no
// mapstructure tag is present, so the knob was inert. Guard the whole path,
// env var -> LoadConfigWithViper -> cfg.Backend.EnableReflection, not just the
// binding.
func TestReflectionEnvVarReachesConfig(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"false", false},
		{"true", true},
		{"1", true},
	}

	for _, tc := range cases {
		t.Run("NOTIFICATOR_GRPC_REFLECTION="+tc.env, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("NOTIFICATOR_GRPC_REFLECTION", tc.env)

			cfg, err := LoadConfigWithViper()
			if err != nil {
				t.Fatalf("LoadConfigWithViper: %v", err)
			}
			if cfg.Backend.EnableReflection != tc.want {
				t.Errorf("EnableReflection = %v, want %v", cfg.Backend.EnableReflection, tc.want)
			}
		})
	}
}

// Same failure mode for the other snake_case backend keys, which docker-compose
// and the Helm chart both set. Assert they survive the unmarshal.
func TestSnakeCaseBackendKeysSurviveUnmarshal(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("backend.grpc_listen", ":60051")
	viper.Set("backend.grpc_client", "backend:60051")
	viper.Set("backend.http_listen", ":9090")
	viper.Set("backend.database.ssl_mode", "require")
	viper.Set("backend.database.sqlite_path", "/tmp/test.db")

	cfg, err := LoadConfigWithViper()
	if err != nil {
		t.Fatalf("LoadConfigWithViper: %v", err)
	}

	for _, tc := range []struct{ name, got, want string }{
		{"grpc_listen", cfg.Backend.GRPCListen, ":60051"},
		{"grpc_client", cfg.Backend.GRPCClient, "backend:60051"},
		{"http_listen", cfg.Backend.HTTPListen, ":9090"},
		{"ssl_mode", cfg.Backend.Database.SSLMode, "require"},
		{"sqlite_path", cfg.Backend.Database.SQLitePath, "/tmp/test.db"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// resetViper wires viper the way cmd/root.go does, so the tests below exercise
// the real loading path instead of a hand-rolled one.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	viper.SetEnvPrefix("NOTIFICATOR")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	t.Cleanup(viper.Reset)
}

// TestMultiWordKeysReachTheStruct is the regression guard for #64: unmarshalling
// used to drop every multi-word key, so backend.allow_registration=false was
// read by viper and then thrown away, leaving the gate permanently open.
func TestMultiWordKeysReachTheStruct(t *testing.T) {
	resetViper(t)
	t.Setenv("NOTIFICATOR_BACKEND_ALLOW_REGISTRATION", "false")
	t.Setenv("NOTIFICATOR_BACKEND_GRPC_LISTEN", ":9999")
	t.Setenv("NOTIFICATOR_BACKEND_DATABASE_SSL_MODE", "require")

	cfg, err := LoadConfigWithViper()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Backend.AllowRegistration {
		t.Error("NOTIFICATOR_BACKEND_ALLOW_REGISTRATION=false was dropped, registration stays open")
	}
	if cfg.Backend.GRPCListen != ":9999" {
		t.Errorf("GRPCListen = %q, want :9999", cfg.Backend.GRPCListen)
	}
	if cfg.Backend.Database.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want require", cfg.Backend.Database.SSLMode)
	}
}

// TestAllowRegistrationFromConfigFile covers the other config source: a JSON
// file must be able to close registration too.
func TestAllowRegistrationFromConfigFile(t *testing.T) {
	resetViper(t)

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"backend":{"enabled":true,"allow_registration":false}}`), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	cfg, err := LoadConfigWithViper()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if !cfg.Backend.Enabled {
		t.Error("backend.enabled from the config file was dropped")
	}
	if cfg.Backend.AllowRegistration {
		t.Error("backend.allow_registration=false from the config file was dropped")
	}
}

// TestAllowRegistrationDefaultsOpen keeps existing deployments working.
func TestAllowRegistrationDefaultsOpen(t *testing.T) {
	resetViper(t)

	cfg, err := LoadConfigWithViper()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if !cfg.Backend.AllowRegistration {
		t.Error("registration must stay enabled when the flag is not set")
	}
}
