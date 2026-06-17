package service

import (
	"testing"
)

// TestAuthSnapshot_PreservesVisibility 守护回归：分组的 Visibility 与 VisiblePlanIDs
// 必须在 auth 缓存快照 往返（snapshotFromAPIKey → snapshotToAPIKey）中保真。
//
// 背景：鉴权热路径经 auth 缓存命中时，Group 由快照还原。若快照遗漏 Visibility/VisiblePlanIDs，
// 中间件的 subscriber 分组校验（Visibility==subscriber 才生效）会被静默跳过，
// 导致"订阅过期拒绝请求"在缓存命中下完全失效。此测试钉死该字段不被丢弃。
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
				User:   &User{ID: 2, Status: StatusActive},
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
		})
	}
}
