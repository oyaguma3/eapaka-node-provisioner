package trace

import (
	"regexp"
	"strings"
	"testing"
)

func TestTraceID(t *testing.T) {
	a, b := New(), New()
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(a) || a == b {
		t.Errorf("New() = %q, %q", a, b)
	}
	if got := From(t.Context()); got != "" {
		t.Errorf("From(empty) = %q", got)
	}
	if got := From(With(t.Context(), a)); got != a {
		t.Errorf("From = %q", got)
	}
}

func TestValid(t *testing.T) {
	for _, id := range []string{"a", "trace-001", "x~!", New(), strings.Repeat("a", 64)} {
		if !Valid(id) {
			t.Errorf("Valid(%q) = false", id)
		}
	}
	for _, id := range []string{"", "has space", "タブ\t", strings.Repeat("a", 65)} {
		if Valid(id) {
			t.Errorf("Valid(%q) = true", id)
		}
	}
}
