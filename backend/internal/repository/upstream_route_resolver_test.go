package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamRouteResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/route-info" && r.Header.Get("x-api-key") == "paratera":
			_, _ = w.Write([]byte(`{"type":"passthrough","upstreams":["https://LLMAPI.paratera.com/v1?k=secret","bad url"]}`))
		case r.URL.Path == "/route-info" && r.Header.Get("x-api-key") == "redirect":
			http.Redirect(w, r, "https://evil.example.com/steal", http.StatusFound)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	r := NewUpstreamRouteResolver()
	ctx := context.Background()

	ups, err := r.ResolveRouteUpstreams(ctx, srv.URL+"/", "paratera")
	require.NoError(t, err)
	require.Equal(t, []string{"https://llmapi.paratera.com"}, ups, "only origins, path/query stripped")

	_, err = r.ResolveRouteUpstreams(ctx, srv.URL, "nope")
	require.ErrorContains(t, err, "401")

	_, err = r.ResolveRouteUpstreams(ctx, srv.URL, "redirect")
	require.ErrorContains(t, err, "302", "redirects are not followed")
}
