//go:build unit

package service

import "testing"

func TestCodexValidator_ValidateUserAgent(t *testing.T) {
	v := NewCodexValidator()
	cases := map[string]bool{
		"codex_cli_rs/0.80.0 (Windows 15.7.2; x86_64) Terminal":                       true,
		"codex_cli_rs/0.0.0":                                                           true,
		"Codex Desktop/0.140.0-alpha.19 (Mac OS 26.5.1; arm64) unknown (Codex Desktop)": true,
		"codex_vscode/0.140.0-alpha.2 (Windows 10.0.26200; x86_64)":                    true,
		"CODEX_CLI_RS/1.2.3":                                                           true, // case-insensitive
		"claude-cli/2.1.0":                                                             false,
		"curl/8.0":                                                                     false,
		"codex_cli_rs":                                                                 false, // no version
		"my-codex-tool/1.0":                                                            false, // not a prefix
		"":                                                                             false,
	}
	for ua, want := range cases {
		if got := v.ValidateUserAgent(ua); got != want {
			t.Errorf("ValidateUserAgent(%q) = %v, want %v", ua, got, want)
		}
	}
}
