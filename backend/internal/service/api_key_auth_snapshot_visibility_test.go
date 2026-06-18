package service

import (
	"testing"
)

// TestAuthSnapshot_PreservesVisibility 守护回归：用户 AllowedGroups 以及分组的
// Visibility 与 VisiblePlanIDs 必须在 auth 缓存快照 往返（snapshotFromAPIKey →
// snapshotToAPIKey）中保真。
//
// 背景：鉴权热路径经 auth 缓存命中时，User/Group 由快照还原。若快照遗漏
// AllowedGroups，管理员显式分配的 subscriber/private 分组会被误拒；若遗漏
// Visibility/VisiblePlanIDs，subscriber 分组的订阅校验会失真。此测试钉死这些字段不被丢弃。
func TestAuthSnapshot_PreservesVisibility(t *testing.T) {
	svc := &APIKeyService{}

	cases := []struct {
		name           string
		visibility     string
		visiblePlanIDs []int64
		wantExclusive  bool
	}{
		{"public", VisibilityPublic, nil, false},
		{"private derives is_exclusive", VisibilityPrivate, nil, true},
		{"subscriber keeps plan ids", VisibilitySubscriber, []int64{7, 42}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := &APIKey{
				ID:     1,
				UserID: 2,
				Status: StatusActive,
				User:   &User{ID: 2, Status: StatusActive, AllowedGroups: []int64{10, 99}},
				Group: &Group{
					ID:             10,
					Name:           "g",
					Platform:       PlatformAnthropic,
					Status:         StatusActive,
					Visibility:     tc.visibility,
					VisiblePlanIDs: tc.visiblePlanIDs,
				},
			}

			snap := svc.snapshotFromAPIKey(orig)
			if snap.Group == nil {
				t.Fatal("snapshot dropped Group")
			}
			if snap.Group.Visibility != tc.visibility {
				t.Fatalf("snapshot Visibility = %q, want %q", snap.Group.Visibility, tc.visibility)
			}
			if len(snap.User.AllowedGroups) != 2 || snap.User.AllowedGroups[0] != 10 || snap.User.AllowedGroups[1] != 99 {
				t.Fatalf("snapshot AllowedGroups = %v, want [10 99]", snap.User.AllowedGroups)
			}

			restored := svc.snapshotToAPIKey("k", snap)
			if restored.Group == nil {
				t.Fatal("restored dropped Group")
			}
			if restored.Group.Visibility != tc.visibility {
				t.Fatalf("restored Visibility = %q, want %q", restored.Group.Visibility, tc.visibility)
			}
			if restored.Group.IsExclusive != tc.wantExclusive {
				t.Fatalf("restored IsExclusive = %v, want %v (derived from visibility)", restored.Group.IsExclusive, tc.wantExclusive)
			}
			if len(restored.Group.VisiblePlanIDs) != len(tc.visiblePlanIDs) {
				t.Fatalf("restored VisiblePlanIDs = %v, want %v", restored.Group.VisiblePlanIDs, tc.visiblePlanIDs)
			}
			for i := range tc.visiblePlanIDs {
				if restored.Group.VisiblePlanIDs[i] != tc.visiblePlanIDs[i] {
					t.Fatalf("restored VisiblePlanIDs = %v, want %v", restored.Group.VisiblePlanIDs, tc.visiblePlanIDs)
				}
			}
			if len(restored.User.AllowedGroups) != 2 || restored.User.AllowedGroups[0] != 10 || restored.User.AllowedGroups[1] != 99 {
				t.Fatalf("restored AllowedGroups = %v, want [10 99]", restored.User.AllowedGroups)
			}
		})
	}
}
