package rules

import "testing"

func TestSelect(t *testing.T) {
	all := []Rule{
		{Key: "k1", Model: "m1", Priority: 10, Order: 0, Filter: "A"},
		{Key: "k1", Priority: 20, Order: 1, Filter: "B"},             // только ключ
		{Key: "k1", Model: "M1", Priority: 5, Order: 2, Filter: "X"}, // другой регистр модели
		{Key: "k2", Priority: 0, Order: 3, Filter: "C"},
	}

	tests := []struct {
		name     string
		key      string
		model    string
		wantTags []string
	}{
		{"точное совпадение ключ+модель — только они", "k1", "m1", []string{"A"}},
		{"модель чувствительна к регистру — fallback на ключ", "k1", "M1", []string{"X"}},
		{"нет модели в запросе — только правила по ключу", "k1", "", []string{"B"}},
		{"другой ключ", "k2", "m1", []string{"C"}},
		{"неизвестный ключ", "nope", "m1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Select(all, tt.key, tt.model)
			if len(got) != len(tt.wantTags) {
				t.Fatalf("получено %d правил (%v), ожидалось %d", len(got), got, len(tt.wantTags))
			}
			for i, r := range got {
				if r.Filter != tt.wantTags[i] {
					t.Errorf("правило #%d: фильтр %q, ожидался %q", i, r.Filter, tt.wantTags[i])
				}
			}
		})
	}
}

func TestSelectPriorityOrder(t *testing.T) {
	all := []Rule{
		{Key: "k", Priority: 20, Order: 0, Filter: "third"},
		{Key: "k", Priority: 0, Order: 1, Filter: "first"},
		{Key: "k", Priority: 10, Order: 2, Filter: "second"},
	}
	got := Select(all, "k", "")
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i].Filter != want[i] {
			t.Fatalf("порядок = %v, ожидался %v", filters(got), want)
		}
	}
}

func TestSelectStableOrderOnEqualPriority(t *testing.T) {
	all := []Rule{
		{Key: "k", Priority: 5, Order: 0, Filter: "manifest-first"},
		{Key: "k", Priority: 5, Order: 1, Filter: "manifest-second"},
		{Key: "k", Priority: 5, Order: 2, Filter: "manifest-third"},
	}
	got := Select(all, "k", "")
	want := []string{"manifest-first", "manifest-second", "manifest-third"}
	for i := range want {
		if got[i].Filter != want[i] {
			t.Fatalf("порядок = %v, ожидался %v", filters(got), want)
		}
	}
}

func TestSelectKeyModelBeatsKeyOnly(t *testing.T) {
	all := []Rule{
		{Key: "k", Priority: 0, Order: 0, Filter: "key-only"},
		{Key: "k", Model: "m", Priority: 100, Order: 1, Filter: "key+model"},
	}
	got := Select(all, "k", "m")
	if len(got) != 1 || got[0].Filter != "key+model" {
		t.Fatalf("ожидалось только правило ключ+модель, получено %v", filters(got))
	}
}

func filters(rs []Rule) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Filter
	}
	return out
}
