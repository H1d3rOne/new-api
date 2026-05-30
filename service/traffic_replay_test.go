package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTrafficReplayRelayPathWhitelist(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "openai v1 root", path: "/v1", want: true},
		{name: "openai v1 endpoint", path: "/v1/chat/completions", want: true},
		{name: "gemini v1beta endpoint", path: "/v1beta/models/gemini:generateContent", want: true},
		{name: "midjourney endpoint", path: "/mj/submit/imagine", want: true},
		{name: "mode midjourney endpoint", path: "/relax/mj/submit/imagine", want: true},
		{name: "suno endpoint", path: "/suno/submit/music", want: true},
		{name: "similar v1 prefix", path: "/v10/chat/completions", want: false},
		{name: "similar mj prefix", path: "/mjx/submit/imagine", want: false},
		{name: "api management mj path", path: "/api/mj/", want: false},
		{name: "dashboard api", path: "/api/traffic/1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTrafficReplayRelayPath(tt.path))
		})
	}
}

func TestTrafficReplayRequestURISanitizesCredentials(t *testing.T) {
	uri, err := trafficReplayRequestURI(&model.TrafficLog{}, "https://example.com/v1/chat/completions?key=a&api_key=b&access_token=c&token=d&model=gpt")
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions?model=gpt", uri)
}

func TestTrafficReplayRequestURIRejectsNonRelayPath(t *testing.T) {
	_, err := trafficReplayRequestURI(&model.TrafficLog{}, "/api/traffic/1?token=secret")
	require.ErrorContains(t, err, "only relay traffic can be replayed")
}
