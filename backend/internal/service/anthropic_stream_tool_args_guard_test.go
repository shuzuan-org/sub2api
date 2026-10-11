package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAnthropicToolArgsGuardLimitsBytes(t *testing.T) {
	guard := newAnthropicToolArgsGuard(4, 0)
	now := time.Now()

	reason, limited := guard.observeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use"}}`, now)
	require.False(t, limited)
	require.Empty(t, reason)

	reason, limited = guard.observeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"abc"}}`, now)
	require.False(t, limited)
	require.Empty(t, reason)

	reason, limited = guard.observeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"de"}}`, now)
	require.True(t, limited)
	require.Equal(t, streamToolArgsLimitBytes, reason)
}

func TestAnthropicToolArgsGuardIgnoresNonToolDeltas(t *testing.T) {
	guard := newAnthropicToolArgsGuard(1, 0)
	now := time.Now()

	_, _ = guard.observeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`, now)
	reason, limited := guard.observeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"this is not tool JSON"}}`, now)
	require.False(t, limited)
	require.Empty(t, reason)
}

func TestAnthropicToolArgsGuardLimitsDuration(t *testing.T) {
	guard := newAnthropicToolArgsGuard(0, 30)
	started := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, limited := guard.observeEvent("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use"}}`, started)
	require.False(t, limited)

	reason, limited := guard.observeEvent("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`, started.Add(31*time.Second))
	require.True(t, limited)
	require.Equal(t, streamToolArgsLimitDuration, reason)
}

func TestAnthropicToolArgsGuardResetsAfterStop(t *testing.T) {
	guard := newAnthropicToolArgsGuard(1, 0)
	now := time.Now()

	_, _ = guard.observeEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use"}}`, now)
	_, _ = guard.observeEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"x"}}`, now)
	_, _ = guard.observeEvent("content_block_stop", `{"type":"content_block_stop","index":0}`, now)

	reason, limited := guard.observeEvent("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use"}}`, now)
	require.False(t, limited)
	require.Empty(t, reason)
}
