package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validYAML = `
listen_addr: ":8080"
upstream_url: "http://127.0.0.1:11434/v1/chat/completions"
timeout: "2s"
rules_file: "rules.yaml"
`

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeTemp(t, validYAML))
	if err != nil {
		t.Fatalf("не ожидал ошибку: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("listen_addr = %q", cfg.ListenAddr)
	}
	if cfg.Timeout != 2*time.Second {
		t.Errorf("timeout = %s", cfg.Timeout)
	}
	if cfg.Debug {
		t.Error("debug должен быть false по умолчанию")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "нет такого.yaml")); err == nil {
		t.Fatal("ожидал ошибку для отсутствующего файла")
	}
}

func TestLoadInvalidCases(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"пустой файл", ""},
		{"невалидный yaml", "listen_addr: [не закрыт"},
		{"нет listen_addr", "upstream_url: \"http://h/p\"\ntimeout: \"1s\"\nrules_file: \"r.yaml\""},
		{"нет upstream_url", "listen_addr: \":1\"\ntimeout: \"1s\"\nrules_file: \"r.yaml\""},
		{"нет timeout", "listen_addr: \":1\"\nupstream_url: \"http://h/p\"\nrules_file: \"r.yaml\""},
		{"нет rules_file", "listen_addr: \":1\"\nupstream_url: \"http://h/p\"\ntimeout: \"1s\""},
		{"таймаут не парсится", "listen_addr: \":1\"\nupstream_url: \"http://h/p\"\ntimeout: \"абракадабра\"\nrules_file: \"r.yaml\""},
		{"таймаут ноль", "listen_addr: \":1\"\nupstream_url: \"http://h/p\"\ntimeout: \"0s\"\nrules_file: \"r.yaml\""},
		{"таймаут отрицательный", "listen_addr: \":1\"\nupstream_url: \"http://h/p\"\ntimeout: \"-1s\"\nrules_file: \"r.yaml\""},
		{"относительный upstream", "listen_addr: \":1\"\nupstream_url: \"just/path\"\ntimeout: \"1s\"\nrules_file: \"r.yaml\""},
		{"неизвестное поле", validYAML + "\nunknown_param: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, tt.yaml)); err == nil {
				t.Fatalf("ожидал ошибку, получили nil")
			}
		})
	}
}

func TestLoadDebugExplicit(t *testing.T) {
	cfg, err := Load(writeTemp(t, validYAML+"debug: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug {
		t.Error("debug должен быть true")
	}
}
