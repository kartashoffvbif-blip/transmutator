// Package config — загрузка и валидация конфигурации сервиса.
package config

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config — проверенная конфигурация сервиса.
type Config struct {
	ListenAddr  string        // адрес прослушивания, например ":8080"
	UpstreamURL string        // полный целевой апстрим-URL
	Timeout     time.Duration // таймаут на выполнение одного правила
	Debug       bool          // подробное логирование
	RulesFile   string        // путь к манифесту правил
}

// rawConfig — конфигурация в том виде, в каком она лежит в YAML.
type rawConfig struct {
	ListenAddr  string `yaml:"listen_addr"`
	UpstreamURL string `yaml:"upstream_url"`
	Timeout     string `yaml:"timeout"`
	Debug       bool   `yaml:"debug"`
	RulesFile   string `yaml:"rules_file"`
}

// Load читает и валидирует конфигурационный файл.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл конфигурации %q: %w", path, err)
	}
	var rc rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // неизвестные ключи — ошибка, чтобы не молча игнорировать опечатки
	if err := dec.Decode(&rc); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("файл конфигурации %q пуст", path)
		}
		return nil, fmt.Errorf("файл конфигурации %q не является корректным YAML: %w", path, err)
	}
	return build(rc)
}

func build(rc rawConfig) (*Config, error) {
	if rc.ListenAddr == "" {
		return nil, fmt.Errorf("не задан обязательный параметр listen_addr")
	}
	if rc.UpstreamURL == "" {
		return nil, fmt.Errorf("не задан обязательный параметр upstream_url")
	}
	if u, err := url.Parse(rc.UpstreamURL); err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("upstream_url %q должен быть абсолютным URL вида http://host:port/...", rc.UpstreamURL)
	}
	if rc.Timeout == "" {
		return nil, fmt.Errorf("не задан обязательный параметр timeout")
	}
	d, err := time.ParseDuration(rc.Timeout)
	if err != nil {
		return nil, fmt.Errorf("timeout %q не является корректной Go-длительностью (например, \"2s\"): %w", rc.Timeout, err)
	}
	if d <= 0 {
		return nil, fmt.Errorf("timeout должен быть больше нуля, получено %s", d)
	}
	if rc.RulesFile == "" {
		return nil, fmt.Errorf("не задан обязательный параметр rules_file")
	}
	return &Config{
		ListenAddr:  rc.ListenAddr,
		UpstreamURL: rc.UpstreamURL,
		Timeout:     d,
		Debug:       rc.Debug,
		RulesFile:   rc.RulesFile,
	}, nil
}
