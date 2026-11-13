//go:build unit

package metrics

import "testing"

func TestUpstreamStatusBucket_502And503AreDistinct(t *testing.T) {
	cases := map[int]string{
		200: "200", 402: "402", 500: "500", 502: "502", 503: "503",
		504: "5xx", 404: "4xx", 204: "2xx", 0: "other",
	}
	for code, want := range cases {
		if got := upstreamStatusBucket(code); got != want {
			t.Errorf("upstreamStatusBucket(%d) = %q, want %q", code, got, want)
		}
	}
}
