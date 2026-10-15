package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRules создаёт во временном каталоге манифест правил и, при необходимости,
// файлы фильтров. Возвращает путь к манифесту.
func writeRules(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validManifest = `
rules:
  - key: "sk-test"
    model: "gpt-4o-mini"
    priority: 10
    filter_inline: |
      .messages = (.messages | safe_truncate_blocks($keep_last))
    vars:
      keep_last: 4

  - key: "sk-test"
    priority: 20
    filter_file: "filters/strip.jq"

  - key: "sk-another"
    filter_inline: |
      .system_prompt = "compact prompt"
`

const validFilter = `.tools |= map(.description = "")`

func TestLoadValid(t *testing.T) {
	path := writeRules(t, validManifest, map[string]string{"filters/strip.jq": validFilter})
	rs, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatalf("не ожидал ошибку: %v", err)
	}
	if len(rs) != 3 {
		t.Fatalf("загружено %d правил, ожидалось 3", len(rs))
	}
	if rs[0].Model != "gpt-4o-mini" || rs[0].Priority != 10 {
		t.Errorf("правило #1: %+v", rs[0])
	}
	if rs[1].Model != "" {
		t.Errorf("пустая модель означает правило только по ключу, получено %q", rs[1].Model)
	}
	if rs[2].Priority != 0 {
		t.Errorf("priority по умолчанию должен быть 0, получено %d", rs[2].Priority)
	}
	if rs[0].Vars["keep_last"] != 4 {
		t.Errorf("переменная keep_last = %v, ожидалось 4", rs[0].Vars["keep_last"])
	}
	if rs[1].Filter != validFilter {
		t.Errorf("фильтр из файла не совпал: %q", rs[1].Filter)
	}
}

func TestLoadEmptyManifest(t *testing.T) {
	// файл есть, правил нет — сервис должен работать чистым прокси
	path := writeRules(t, "rules: []\n", nil)
	rs, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatalf("пустой список правил не должен быть ошибкой: %v", err)
	}
	if len(rs) != 0 {
		t.Errorf("ожидался пустой список, получено %d", len(rs))
	}
}

func TestLoadEmptyFile(t *testing.T) {
	path := writeRules(t, "", nil)
	if _, err := NewFileStore(path).Load(); err != nil {
		t.Fatalf("пустой файл правил не должен быть ошибкой: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := NewFileStore(filepath.Join(t.TempDir(), "нет такого.yaml")).Load(); err == nil {
		t.Fatal("ожидал ошибку для отсутствующего файла правил")
	}
}

func TestLoadInvalidRules(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
	}{
		{
			name:     "невалидный yaml",
			manifest: "rules: [{",
		},
		{
			name:     "неизвестное поле",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \".\"\n    unknown: 1\n",
		},
		{
			name:     "пустой ключ",
			manifest: "rules:\n  - key: \"\"\n    filter_inline: \".\"\n",
		},
		{
			name:     "нет ключа",
			manifest: "rules:\n  - filter_inline: \".\"\n",
		},
		{
			name:     "два фильтра сразу",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \".\"\n    filter_file: \"f.jq\"\n",
			files:    map[string]string{"f.jq": "."},
		},
		{
			name:     "нет фильтра",
			manifest: "rules:\n  - key: \"k\"\n",
		},
		{
			name:     "фильтр не парсится",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \".foo |\"\n",
		},
		{
			name:     "фильтр не компилируется",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \"$unknown_var\"\n",
		},
		{
			name:     "отсутствующий файл фильтра",
			manifest: "rules:\n  - key: \"k\"\n    filter_file: \"нет.jq\"\n",
		},
		{
			name:     "невалидное имя переменной",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \".\"\n    vars:\n      \"не-имя\": 1\n",
		},
		{
			name:     "неизвестная переменная в фильтре",
			manifest: "rules:\n  - key: \"k\"\n    filter_inline: \"$x\"\n    vars:\n      y: 1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeRules(t, tt.manifest, tt.files)
			if _, err := NewFileStore(path).Load(); err == nil {
				t.Fatal("ожидал ошибку загрузки правил")
			}
		})
	}
}

func TestLoadFilterFileRelativeToManifest(t *testing.T) {
	// filter_file должен искаться относительно каталога манифеста
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.jq"), []byte(".tag = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "rules:\n  - key: \"k\"\n    filter_file: \"../f.jq\"\n"
	if err := os.WriteFile(filepath.Join(sub, "rules.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := NewFileStore(filepath.Join(sub, "rules.yaml")).Load()
	if err != nil {
		t.Fatalf("относительный путь ../f.jq должен находиться: %v", err)
	}
	if rs[0].Filter != ".tag = 1" {
		t.Errorf("фильтр = %q", rs[0].Filter)
	}
}

func TestLoadGroupFilters(t *testing.T) {
	m := `
rules:
  - key: "sk-group"
    model: "m1"
    filters:
      - priority: 1
        filter_file: "filters/a.jq"
      - priority: 2
        filter_inline: |
          .x = 1
`
	path := writeRules(t, m, map[string]string{"filters/a.jq": validFilter})
	rs, err := NewFileStore(path).Load()
	if err != nil {
		t.Fatalf("не ожидал ошибку: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("загружено %d правил, ожидалось 2", len(rs))
	}
	if rs[0].Key != "sk-group" || rs[0].Model != "m1" || rs[0].Priority != 1 {
		t.Errorf("первое правило: key=%q model=%q prio=%d", rs[0].Key, rs[0].Model, rs[0].Priority)
	}
	if rs[1].Priority != 2 || rs[1].Model != "m1" {
		t.Errorf("второе правило: prio=%d model=%q", rs[1].Priority, rs[1].Model)
	}
}

func TestLoadGroupFiltersInvalid(t *testing.T) {
	cases := map[string]string{
		"фильтр на уровне группы": `
rules:
  - key: "k"
    filter_file: "filters/a.jq"
    filters:
      - filter_inline: ".x = 1"
`,
		"priority на уровне группы": `
rules:
  - key: "k"
    priority: 5
    filters:
      - filter_inline: ".x = 1"
`,
		"key внутри элемента": `
rules:
  - key: "k"
    filters:
      - key: "other"
        filter_inline: ".x = 1"
`,
		"пустой элемент": `
rules:
  - key: "k"
    filters:
      - {}
`,
	}
	for name, m := range cases {
		path := writeRules(t, m, map[string]string{"filters/a.jq": validFilter})
		if _, err := NewFileStore(path).Load(); err == nil {
			t.Errorf("%s: ожидалась ошибка, получен успех", name)
		}
	}
}
