package service

import (
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	streamToolArgsLimitBytes    = "bytes"
	streamToolArgsLimitDuration = "duration"
)

// anthropicToolArgsGuard limits one Anthropic tool-use block. The guard only
// counts input_json_delta payloads that belong to an open tool_use block; it
// does not attempt to split or synthesize JSON that the upstream did not send.
type anthropicToolArgsGuard struct {
	maxBytes    int64
	maxDuration time.Duration

	activeIndex int
	startedAt   time.Time
	bytes       int64
}

func newAnthropicToolArgsGuard(maxBytes int, maxDurationSeconds int) *anthropicToolArgsGuard {
	return &anthropicToolArgsGuard{
		maxBytes:    int64(maxBytes),
		maxDuration: time.Duration(maxDurationSeconds) * time.Second,
		activeIndex: -1,
	}
}

// observe returns a bounded reason when the current tool-use block exceeds a
// configured limit. A disabled limit is represented by a non-positive value.
func (g *anthropicToolArgsGuard) observe(eventType string, index int, blockType, deltaType, partialJSON string, now time.Time) (string, bool) {
	if g == nil {
		return "", false
	}

	switch eventType {
	case "message_start":
		g.reset()
	case "content_block_start":
		// At most one content block is active in the Anthropic stream contract.
		g.reset()
		if blockType == "tool_use" && index >= 0 {
			g.activeIndex = index
			g.startedAt = now
		}
	case "content_block_stop":
		if index == g.activeIndex {
			g.reset()
		}
	case "content_block_delta":
		if index != g.activeIndex || deltaType != "input_json_delta" {
			return "", false
		}
		g.bytes += int64(len(partialJSON))
		if g.maxBytes > 0 && g.bytes > g.maxBytes {
			return streamToolArgsLimitBytes, true
		}
		if g.maxDuration > 0 && !g.startedAt.IsZero() && now.Sub(g.startedAt) > g.maxDuration {
			return streamToolArgsLimitDuration, true
		}
	}

	return "", false
}

func (g *anthropicToolArgsGuard) observeEvent(eventType string, payload string, now time.Time) (string, bool) {
	if payload == "" || !strings.HasPrefix(payload, "{") {
		return "", false
	}
	index := -1
	if value := gjson.Get(payload, "index"); value.Exists() {
		index = int(value.Int())
	}
	blockType := ""
	if eventType == "content_block_start" {
		blockType = gjson.Get(payload, "content_block.type").String()
	}
	deltaType := ""
	partialJSON := ""
	if eventType == "content_block_delta" {
		deltaType = gjson.Get(payload, "delta.type").String()
		partialJSON = gjson.Get(payload, "delta.partial_json").String()
	}
	return g.observe(eventType, index, blockType, deltaType, partialJSON, now)
}

func (g *anthropicToolArgsGuard) reset() {
	g.activeIndex = -1
	g.startedAt = time.Time{}
	g.bytes = 0
}

func (g *anthropicToolArgsGuard) expired(now time.Time) (string, bool) {
	if g == nil || g.activeIndex < 0 || g.maxDuration <= 0 || g.startedAt.IsZero() {
		return "", false
	}
	if now.Sub(g.startedAt) > g.maxDuration {
		return streamToolArgsLimitDuration, true
	}
	return "", false
}
