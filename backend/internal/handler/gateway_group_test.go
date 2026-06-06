package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayGroupReturnsCurrentAPIKeyGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/v1/group", func(c *gin.Context) {
		groupID := int64(10)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      100,
			GroupID: &groupID,
			Group: &service.Group{
				ID:             groupID,
				Name:           "Group One",
				Description:    "desc",
				Platform:       service.PlatformAnthropic,
				RateMultiplier: 1.5,
				Status:         service.StatusActive,
				ModelRouting: map[string][]int64{
					"claude-3-*": []int64{101, 102},
				},
				AccountCount: 2,
			},
		})
		(&GatewayHandler{}).Group(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/group", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{
		"id": 10,
		"name": "Group One",
		"description": "desc",
		"platform": "anthropic",
		"rate_multiplier": 1.5,
		"is_exclusive": false,
		"status": "active",
		"image_price_1k": null,
		"image_price_2k": null,
		"image_price_4k": null,
		"sora_image_price_360": null,
		"sora_image_price_540": null,
		"sora_video_price_per_request": null,
		"sora_video_price_per_request_hd": null,
		"claude_code_only": false,
		"fallback_group_id": null,
		"fallback_group_id_on_invalid_request": null,
		"sora_storage_quota_bytes": 0,
		"allow_messages_dispatch": false,
		"created_at": "0001-01-01T00:00:00Z",
		"updated_at": "0001-01-01T00:00:00Z"
	}`, w.Body.String())
}

func TestGatewayGroupReturnsNotFoundForUngroupedAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/v1/group", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 100})
		(&GatewayHandler{}).Group(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/group", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{
		"type": "error",
		"error": {
			"type": "not_found_error",
			"message": "API key is not assigned to any group"
		}
	}`, w.Body.String())
}
