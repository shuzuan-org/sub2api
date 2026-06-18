package service

import "regexp"

// CodexValidator detects the OpenAI Codex client family by User-Agent, mirroring
// ClaudeCodeValidator. Codex ships three UA shapes seen in production:
//
//	codex_cli_rs/0.80.0 (Windows 15.7.2; x86_64) Terminal
//	Codex Desktop/0.140.0-alpha.19 (Mac OS 26.5.1; arm64) unknown (Codex Desktop; ...)
//	codex_vscode/0.140.0-alpha.2 (Windows 10.0.26200; x86_64) unknown (VS Code; ...)
//
// Like the Claude check, this is a content-negotiation signal for GET /v1/models
// (which model names to surface), NOT an auth gate — auth is the API key. A spoofed
// Codex UA only changes which name view the caller sees.
type CodexValidator struct{}

// codexUAPattern matches the three Codex UA prefixes, case-insensitively. The version
// is required so a bare "codex" substring elsewhere doesn't false-positive.
var codexUAPattern = regexp.MustCompile(`(?i)^(codex_cli_rs|codex_vscode|codex desktop)/\d+\.\d+\.\d+`)

// NewCodexValidator creates a validator instance.
func NewCodexValidator() *CodexValidator { return &CodexValidator{} }

// ValidateUserAgent reports whether the User-Agent is a recognized Codex client.
func (v *CodexValidator) ValidateUserAgent(ua string) bool {
	return codexUAPattern.MatchString(ua)
}
