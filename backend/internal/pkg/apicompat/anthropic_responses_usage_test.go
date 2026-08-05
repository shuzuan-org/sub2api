package apicompat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Anthropic reports input_tokens excluding whatever was read from or written to
// the cache; OpenAI reports the whole prompt in input_tokens/prompt_tokens with
// cached_tokens as a breakdown inside it. These tests pin the conversion so a
// cache hit no longer deflates the OpenAI-facing count.

func TestAnthropicToResponsesResponse_InputTokensIncludeCache(t *testing.T) {
	resp := &AnthropicResponse{
		ID:    "msg_1",
		Model: "claude-sonnet-4-6",
		Role:  "assistant",
		Usage: AnthropicUsage{
			InputTokens:              100,
			OutputTokens:             25,
			CacheReadInputTokens:     900,
			CacheCreationInputTokens: 50,
		},
	}

	out := AnthropicToResponsesResponse(resp)
	require.NotNil(t, out.Usage)

	assert.Equal(t, 1050, out.Usage.InputTokens, "input_tokens must cover the whole prompt (100 fresh + 900 cache read + 50 cache write)")
	assert.Equal(t, 25, out.Usage.OutputTokens)
	assert.Equal(t, 1075, out.Usage.TotalTokens)
	require.NotNil(t, out.Usage.InputTokensDetails)
	assert.Equal(t, 900, out.Usage.InputTokensDetails.CachedTokens)
	assert.LessOrEqual(t, out.Usage.InputTokensDetails.CachedTokens, out.Usage.InputTokens,
		"cached_tokens is a subset of input_tokens, never larger")
}

func TestAnthropicToResponsesResponse_NoCacheIsUnchanged(t *testing.T) {
	resp := &AnthropicResponse{
		ID:    "msg_2",
		Model: "claude-sonnet-4-6",
		Role:  "assistant",
		Usage: AnthropicUsage{InputTokens: 100, OutputTokens: 25},
	}

	out := AnthropicToResponsesResponse(resp)
	require.NotNil(t, out.Usage)
	assert.Equal(t, 100, out.Usage.InputTokens)
	assert.Equal(t, 125, out.Usage.TotalTokens)
	assert.Nil(t, out.Usage.InputTokensDetails)
}

func TestAnthropicStreamToResponses_InputTokensIncludeCacheFromMessageStart(t *testing.T) {
	state := NewAnthropicEventToResponsesState()

	// Native Anthropic streams carry the cache breakdown in message_start.
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_3",
			Model: "claude-sonnet-4-6",
			Role:  "assistant",
			Usage: AnthropicUsage{
				InputTokens:              100,
				CacheReadInputTokens:     900,
				CacheCreationInputTokens: 50,
			},
		},
	}, state)
	AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "message_delta",
		Usage: &AnthropicUsage{OutputTokens: 25},
	}, state)
	events := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_stop"}, state)

	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
		}
	}
	require.NotNil(t, completed, "expected a response.completed event")
	require.NotNil(t, completed.Response)
	require.NotNil(t, completed.Response.Usage)

	usage := completed.Response.Usage
	assert.Equal(t, 1050, usage.InputTokens)
	assert.Equal(t, 25, usage.OutputTokens)
	assert.Equal(t, 1075, usage.TotalTokens)
	require.NotNil(t, usage.InputTokensDetails)
	assert.Equal(t, 900, usage.InputTokensDetails.CachedTokens)
}

// The Anthropic → Responses → Chat Completions chain is what an OpenAI-protocol
// client actually receives, so assert the contract at that boundary too.
func TestAnthropicToChatCompletions_PromptTokensIncludeCached(t *testing.T) {
	resp := &AnthropicResponse{
		ID:    "msg_4",
		Model: "claude-sonnet-4-6",
		Role:  "assistant",
		Usage: AnthropicUsage{
			InputTokens:          100,
			OutputTokens:         25,
			CacheReadInputTokens: 900,
		},
	}

	chat := ResponsesToChatCompletions(AnthropicToResponsesResponse(resp), "glm-5.2")
	require.NotNil(t, chat.Usage)

	assert.Equal(t, 1000, chat.Usage.PromptTokens)
	assert.Equal(t, 25, chat.Usage.CompletionTokens)
	assert.Equal(t, 1025, chat.Usage.TotalTokens)
	require.NotNil(t, chat.Usage.PromptTokensDetails)
	assert.Equal(t, 900, chat.Usage.PromptTokensDetails.CachedTokens)
	assert.LessOrEqual(t, chat.Usage.PromptTokensDetails.CachedTokens, chat.Usage.PromptTokens)
}
