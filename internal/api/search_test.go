package api

import (
	"context"
	"testing"
)

// One search per session: a new one ends the running one, and another
// session's is left alone.
func TestOneSearchPerSession(t *testing.T) {
	s := newSearches()
	first, doneFirst := s.begin("alex", context.Background())
	other, doneOther := s.begin("sam", context.Background())
	second, doneSecond := s.begin("alex", context.Background())
	if first.Err() == nil {
		t.Fatal("the session's earlier search still runs")
	}
	if second.Err() != nil || other.Err() != nil {
		t.Fatal("a search that should run was ended")
	}
	// The earlier one ending does not forget the newer one.
	doneFirst()
	if s.running["alex"] == nil {
		t.Fatal("the running search was forgotten")
	}
	doneSecond()
	doneOther()
	if len(s.running) != 0 {
		t.Fatalf("%d searches left behind", len(s.running))
	}
}
