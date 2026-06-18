package service

import "testing"

func TestUser_CanBindGroup(t *testing.T) {
	const groupID int64 = 42

	tests := []struct {
		name             string
		allowedGroups    []int64
		visibility       string
		groupVisiblePlan []int64
		userActivePlan   []int64
		want             bool
	}{
		{
			name:       "public visible to everyone",
			visibility: VisibilityPublic,
			want:       true,
		},
		{
			name:          "private with membership",
			visibility:    VisibilityPrivate,
			allowedGroups: []int64{7, 42, 99},
			want:          true,
		},
		{
			name:          "private without membership",
			visibility:    VisibilityPrivate,
			allowedGroups: []int64{7, 99},
			want:          false,
		},
		{
			name:             "subscriber with matching plan",
			visibility:       VisibilitySubscriber,
			groupVisiblePlan: []int64{1, 2, 3},
			userActivePlan:   []int64{9, 2},
			want:             true,
		},
		{
			name:             "subscriber without matching plan",
			visibility:       VisibilitySubscriber,
			groupVisiblePlan: []int64{1, 2, 3},
			userActivePlan:   []int64{9, 8},
			want:             false,
		},
		{
			name:             "subscriber but group binds no plan",
			visibility:       VisibilitySubscriber,
			groupVisiblePlan: nil,
			userActivePlan:   []int64{1, 2},
			want:             false,
		},
		{
			name:             "subscriber but user has no active subscription",
			visibility:       VisibilitySubscriber,
			groupVisiblePlan: []int64{1, 2},
			userActivePlan:   nil,
			want:             false,
		},
		{
			name:       "unknown visibility denied",
			visibility: "weird",
			want:       false,
		},
		{
			// Bug fix: an admin assignment overrides subscriber — an assigned user can use
			// a subscriber group even with no matching subscription.
			name:             "subscriber but assigned overrides (no subscription)",
			visibility:       VisibilitySubscriber,
			allowedGroups:    []int64{42},
			groupVisiblePlan: []int64{1, 2},
			userActivePlan:   nil,
			want:             true,
		},
		{
			// Assignment also overrides an empty/unknown visibility (which otherwise denies).
			name:          "unknown visibility but assigned overrides",
			visibility:    "",
			allowedGroups: []int64{42},
			want:          true,
		},
		{
			name:          "assignment to a different group does not grant access",
			visibility:    VisibilitySubscriber,
			allowedGroups: []int64{7, 99},
			userActivePlan: nil,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{AllowedGroups: tt.allowedGroups}
			got := u.CanBindGroup(groupID, tt.visibility, tt.groupVisiblePlan, tt.userActivePlan)
			if got != tt.want {
				t.Fatalf("CanBindGroup(%q) = %v, want %v", tt.visibility, got, tt.want)
			}
		})
	}
}

func TestResolveVisibility(t *testing.T) {
	tests := []struct {
		name        string
		visibility  string
		isExclusive bool
		want        string
	}{
		{"explicit public", VisibilityPublic, true, VisibilityPublic},
		{"explicit subscriber", VisibilitySubscriber, false, VisibilitySubscriber},
		{"explicit private", VisibilityPrivate, false, VisibilityPrivate},
		{"empty falls back to private when exclusive", "", true, VisibilityPrivate},
		{"empty falls back to public when not exclusive", "", false, VisibilityPublic},
		{"invalid falls back via exclusive", "bogus", true, VisibilityPrivate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVisibility(tt.visibility, tt.isExclusive); got != tt.want {
				t.Fatalf("ResolveVisibility(%q, %v) = %q, want %q", tt.visibility, tt.isExclusive, got, tt.want)
			}
		})
	}
}
