package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrOAuthProfileRequired  = ErrOAuthInvalidRequest
	ErrOAuthProfileForbidden = infraerrors.Forbidden("OAUTH_PROFILE_FORBIDDEN", "oauth profile is not available")
)

type OAuthProfile struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Status      string  `json:"status"`
	Platform    string  `json:"platform"`
	GroupID     int64   `json:"group_id"`
	GroupName   string  `json:"group_name"`
	ExpiresAt   *string `json:"expires_at"`
	IsDefault   bool    `json:"is_default"`
}

type OAuthProfileService struct {
	apiKeyRepo APIKeyRepository
	userRepo   UserRepository
}

func NewOAuthProfileService(apiKeyRepo APIKeyRepository, userRepo UserRepository) *OAuthProfileService {
	return &OAuthProfileService{apiKeyRepo: apiKeyRepo, userRepo: userRepo}
}

func (s *OAuthProfileService) ListProfiles(ctx context.Context, userID int64) ([]OAuthProfile, error) {
	if s == nil || s.apiKeyRepo == nil || s.userRepo == nil || userID <= 0 {
		return nil, ErrOAuthInvalidRequest
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive() {
		return nil, ErrUserNotActive
	}
	keys, _, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{Page: 1, PageSize: 1000}, APIKeyListFilters{Status: StatusAPIKeyActive})
	if err != nil {
		return nil, fmt.Errorf("list oauth profiles: %w", err)
	}
	profiles := make([]OAuthProfile, 0, len(keys))
	for i := range keys {
		key := keys[i]
		if !isAPIKeyUsableOAuthProfileForList(&key) {
			continue
		}
		profile := OAuthProfile{
			ID:          strconv.FormatInt(key.ID, 10),
			Name:        key.Name,
			DisplayName: oauthProfileDisplayName(&key),
			Status:      key.Status,
			Platform:    key.Group.Platform,
			GroupID:     key.Group.ID,
			GroupName:   key.Group.Name,
			IsDefault:   len(profiles) == 0,
		}
		if key.ExpiresAt != nil {
			value := key.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
			profile.ExpiresAt = &value
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *OAuthProfileService) ResolveAPIKeyProfile(ctx context.Context, userID int64, profileID string) (*APIKey, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, ErrOAuthProfileRequired
	}
	apiKeyID, err := strconv.ParseInt(profileID, 10, 64)
	if err != nil || apiKeyID <= 0 {
		return nil, ErrOAuthProfileForbidden
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.UserID != userID || !isAPIKeyUsableOAuthProfile(apiKey) {
		return nil, ErrOAuthProfileForbidden
	}
	return apiKey, nil
}

func isAPIKeyUsableOAuthProfileForList(key *APIKey) bool {
	return key != nil &&
		key.IsActive() &&
		!key.IsExpired() &&
		!key.IsQuotaExhausted() &&
		key.GroupID != nil &&
		key.Group != nil &&
		key.Group.IsActive()
}

func isAPIKeyUsableOAuthProfile(key *APIKey) bool {
	return key != nil &&
		key.User != nil &&
		key.User.IsActive() &&
		key.IsActive() &&
		!key.IsExpired() &&
		!key.IsQuotaExhausted() &&
		key.GroupID != nil &&
		key.Group != nil &&
		key.Group.IsActive()
}

func oauthProfileDisplayName(key *APIKey) string {
	name := strings.TrimSpace(key.Name)
	if name == "" {
		name = fmt.Sprintf("API Key #%d", key.ID)
	}
	if key.Group == nil || strings.TrimSpace(key.Group.Name) == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, key.Group.Name)
}
