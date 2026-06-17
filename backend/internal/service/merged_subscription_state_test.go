package service

import (
	"testing"
	"time"
)

func TestMergedSubscriptionState_ActivePlanIDs(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)
	past := now.Add(-time.Hour)

	t.Run("nil state", func(t *testing.T) {
		var s *MergedSubscriptionState
		if got := s.ActivePlanIDs(); got != nil {
			t.Fatalf("nil state ActivePlanIDs = %v, want nil", got)
		}
	})

	t.Run("filters expired and dedups", func(t *testing.T) {
		s := &MergedSubscriptionState{
			FIFOQueue: []UserSubscription{
				{PlanID: 1, ExpiresAt: future},
				{PlanID: 2, ExpiresAt: past}, // expired -> excluded
				{PlanID: 1, ExpiresAt: future}, // duplicate plan -> deduped
				{PlanID: 3, ExpiresAt: future},
			},
		}
		got := s.ActivePlanIDs()
		want := map[int64]bool{1: true, 3: true}
		if len(got) != len(want) {
			t.Fatalf("ActivePlanIDs = %v, want plans %v", got, want)
		}
		for _, id := range got {
			if !want[id] {
				t.Fatalf("ActivePlanIDs returned unexpected plan %d (got %v)", id, got)
			}
		}
	})

	t.Run("all expired yields empty", func(t *testing.T) {
		s := &MergedSubscriptionState{
			FIFOQueue: []UserSubscription{
				{PlanID: 1, ExpiresAt: past},
				{PlanID: 2, ExpiresAt: past},
			},
		}
		if got := s.ActivePlanIDs(); len(got) != 0 {
			t.Fatalf("ActivePlanIDs = %v, want empty", got)
		}
	})
}
