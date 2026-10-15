package logging

import (
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	a := "line1\nline2\nline3\nline4"
	b := "line1\nCHANGED\nline3\nline4"
	got := Diff(a, b)
	want := []string{"- line2", "+ CHANGED"}
	if len(got) != len(want) {
		t.Fatalf("diff = %v, ожидался %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diff[%d] = %q, ожидался %q", i, got[i], want[i])
		}
	}
}

func TestDiffIdentical(t *testing.T) {
	if d := Diff("a\nb", "a\nb"); len(d) != 0 {
		t.Errorf("одинаковые тексты должны давать пустой diff: %v", d)
	}
}

func TestDiffInsertion(t *testing.T) {
	d := strings.Join(Diff("a\nb", "a\nnew\nb"), ";")
	if !strings.Contains(d, "+ new") || strings.Contains(d, "- ") {
		t.Errorf("вставка должна давать только +: %s", d)
	}
}
