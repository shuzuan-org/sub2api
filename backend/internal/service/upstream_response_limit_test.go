package service

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func gzipBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(p)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func deflateBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = w.Write(p)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func zstdBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	_, err = w.Write(p)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func brotliBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	_, err := w.Write(p)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// TestReadUpstreamResponseBodyWithHeaders_Decompress is the regression guard for the
// desktop-Claude 502: a zstd/br upstream body must be decoded to JSON, not handed to the
// parser raw. Covers every Content-Encoding plus header-less magic-byte sniffing.
func TestReadUpstreamResponseBodyWithHeaders_Decompress(t *testing.T) {
	const payload = `{"usage":{"input_tokens":1}}`
	const maxBytes = int64(1 << 20)

	cases := []struct {
		name     string
		encoding string // Content-Encoding header value
		body     []byte
	}{
		{"identity", "", []byte(payload)},
		{"gzip", "gzip", gzipBytes(t, []byte(payload))},
		{"deflate", "deflate", deflateBytes(t, []byte(payload))},
		{"zstd", "zstd", zstdBytes(t, []byte(payload))},
		{"brotli", "br", brotliBytes(t, []byte(payload))},
		{"zstd uppercase header", "ZSTD", zstdBytes(t, []byte(payload))},
		// No header at all — must be sniffed from the magic bytes.
		{"gzip sniffed no header", "", gzipBytes(t, []byte(payload))},
		{"zstd sniffed no header", "", zstdBytes(t, []byte(payload))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.encoding != "" {
				h.Set("Content-Encoding", tc.encoding)
			}
			got, err := readUpstreamResponseBodyWithHeaders(bytes.NewReader(tc.body), h, maxBytes)
			require.NoError(t, err)
			require.Equal(t, payload, string(got))
		})
	}
}

func TestNewDecompressingReader(t *testing.T) {
	const payload = "event: message\ndata: {}\n\n"

	t.Run("identity passes reader through untouched", func(t *testing.T) {
		src := bytes.NewReader([]byte(payload))
		r, closeFn, err := newDecompressingReader(src, "")
		require.NoError(t, err)
		defer closeFn()
		out, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, payload, string(out))
	})

	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"gzip", "gzip", gzipBytes(t, []byte(payload))},
		{"deflate", "deflate", deflateBytes(t, []byte(payload))},
		{"zstd", "zstd", zstdBytes(t, []byte(payload))},
		{"brotli", "br", brotliBytes(t, []byte(payload))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, closeFn, err := newDecompressingReader(bytes.NewReader(tc.body), tc.encoding)
			require.NoError(t, err)
			defer closeFn()
			out, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, payload, string(out))
		})
	}
}

func TestResolveUpstreamResponseReadLimit(t *testing.T) {
	t.Run("use default when config missing", func(t *testing.T) {
		require.Equal(t, defaultUpstreamResponseReadMaxBytes, resolveUpstreamResponseReadLimit(nil))
	})

	t.Run("use configured value", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.UpstreamResponseReadMaxBytes = 1234
		require.Equal(t, int64(1234), resolveUpstreamResponseReadLimit(cfg))
	})
}

func TestReadUpstreamResponseBodyLimited(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		body, err := readUpstreamResponseBodyLimited(bytes.NewReader([]byte("ok")), 2)
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), body)
	})

	t.Run("exceeds limit", func(t *testing.T) {
		body, err := readUpstreamResponseBodyLimited(bytes.NewReader([]byte("toolong")), 3)
		require.Nil(t, body)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrUpstreamResponseBodyTooLarge))
	})
}
