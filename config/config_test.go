package config

import (
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
