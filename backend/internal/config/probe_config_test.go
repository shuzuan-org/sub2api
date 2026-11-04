package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeConfigValidate(t *testing.T) {
	base := func() ProbeConfig {
		return ProbeConfig{Enabled: true, BaseURL: "http://127.0.0.1:8080", APIKey: "sk-x", IntervalSeconds: 600, TimeoutSeconds: 60, MaxTokens: 8, MeteringDelaySeconds: 15,
			Targets: []ProbeTarget{{Model: "m"}}}
	}
	ok := base()
	require.NoError(t, ok.validate())

	disabled := ProbeConfig{}
	require.NoError(t, disabled.validate(), "disabled probe needs no other fields")

	fast := base()
	fast.IntervalSeconds = 10
	require.Error(t, fast.validate())

	noKey := base()
	noKey.APIKey = ""
	require.Error(t, noKey.validate())
	noKey.Targets[0].APIKey = "sk-target"
	require.NoError(t, noKey.validate(), "per-target key is enough")

	badProto := base()
	badProto.Targets[0].Protocol = "gemini"
	require.Error(t, badProto.validate())

	noModel := base()
	noModel.Targets[0].Model = " "
	require.Error(t, noModel.validate())
}
