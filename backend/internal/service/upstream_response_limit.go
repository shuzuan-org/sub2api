package service

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrUpstreamResponseBodyTooLarge = errors.New("upstream response body too large")

const defaultUpstreamResponseReadMaxBytes int64 = 8 * 1024 * 1024

func resolveUpstreamResponseReadLimit(cfg *config.Config) int64 {
	if cfg != nil && cfg.Gateway.UpstreamResponseReadMaxBytes > 0 {
		return cfg.Gateway.UpstreamResponseReadMaxBytes
	}
	return defaultUpstreamResponseReadMaxBytes
}

// readUpstreamResponseBody is called with the response headers available.
// Use this version in contexts where headers are accessible.
func readUpstreamResponseBodyWithHeaders(reader io.Reader, headers http.Header, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body is nil")
	}
	if maxBytes <= 0 {
		maxBytes = defaultUpstreamResponseReadMaxBytes
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}

	// Handle Content-Encoding as per HTTP spec. We forward the client's Accept-Encoding
	// verbatim to the upstream (see allowedHeaders), which disables Go's transparent gzip
	// decompression, so EVERY encoding the upstream may pick must be handled here — gzip,
	// deflate, zstd and br. A browser/Electron client (e.g. the Claude desktop app) offers
	// br+zstd, and a zstd-capable relay then replies Content-Encoding: zstd; missing that
	// case left the raw zstd frame to be JSON-parsed → "invalid character" → 502.
	encoding := strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding")))
	// Some upstreams compress the body but omit/mislabel Content-Encoding. A valid JSON
	// body never begins with a compression magic number, so sniff as a fallback.
	if encoding == "" {
		encoding = sniffContentEncoding(body)
	}
	switch encoding {
	case "gzip":
		decompressed, err := decompressGzip(body, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress gzip response: %w", err)
		}
		return decompressed, nil
	case "deflate":
		decompressed, err := decompressDeflate(body, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress deflate response: %w", err)
		}
		return decompressed, nil
	case "zstd":
		decompressed, err := decompressZstd(body, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress zstd response: %w", err)
		}
		return decompressed, nil
	case "br":
		decompressed, err := decompressBrotli(body, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress brotli response: %w", err)
		}
		return decompressed, nil
	default:
		// No encoding or an unknown one — return as-is, let caller handle.
		return body, nil
	}
}

// sniffContentEncoding detects gzip/zstd compression from the leading magic bytes, for
// upstreams that send a compressed body without a Content-Encoding header. It only reports
// encodings with an unambiguous magic number (gzip, zstd); deflate and brotli have none, so
// they require the header. Returns "" when the body is not a recognized compressed frame.
func sniffContentEncoding(body []byte) string {
	switch {
	case len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b:
		return "gzip"
	case len(body) >= 4 && body[0] == 0x28 && body[1] == 0xb5 && body[2] == 0x2f && body[3] == 0xfd:
		return "zstd"
	default:
		return ""
	}
}

// readUpstreamResponseBodyLimited is the fallback for contexts without headers.
// Use readUpstreamResponseBodyWithHeaders when headers are available.
func readUpstreamResponseBodyLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body is nil")
	}
	if maxBytes <= 0 {
		maxBytes = defaultUpstreamResponseReadMaxBytes
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}

	return body, nil
}

// newDecompressingReader wraps r with a streaming decompressor matching the upstream
// Content-Encoding, for the SSE/streaming path (which scans resp.Body line by line and
// cannot buffer-then-decompress). It exists because we forward the client's Accept-Encoding
// to the upstream AND strip Content-Encoding from the response we write back, so a compressed
// stream that is not decoded here reaches the client as undecodable bytes.
//
// An empty/identity/unknown encoding returns r unchanged with a no-op closer, so the common
// case — uncompressed SSE — is completely untouched. The returned close func releases codec
// resources (real only for gzip/zstd) and must be deferred by the caller.
func newDecompressingReader(r io.Reader, contentEncoding string) (io.Reader, func(), error) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "gzip":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, func() {}, err
		}
		return gr, func() { _ = gr.Close() }, nil
	case "deflate":
		fr := flate.NewReader(r)
		return fr, func() { _ = fr.Close() }, nil
	case "zstd":
		// WithDecoderConcurrency(1): a streaming SSE body is consumed by a single scanner
		// goroutine while the caller's deferred Close can fire from another goroutine on a
		// mid-stream client disconnect. The default multi-goroutine zstd decoder tears down
		// internal channels on Close, which races an in-flight Read; a concurrency-1 decoder
		// decodes synchronously in Read (no background workers), keeping that window benign.
		// (Streaming upstreams don't compress SSE in practice today — this path is defensive
		// — but the safe decoder is the correct shape if one ever does.)
		zr, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, func() {}, err
		}
		return zr, zr.Close, nil
	case "br":
		return brotli.NewReader(r), func() {}, nil
	default:
		return r, func() {}, nil
	}
}

func decompressGzip(body []byte, maxBytes int64) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decompressed, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > maxBytes {
		return nil, fmt.Errorf("%w: decompressed limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}
	return decompressed, nil
}

func decompressDeflate(body []byte, maxBytes int64) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(body))
	defer reader.Close()
	decompressed, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > maxBytes {
		return nil, fmt.Errorf("%w: decompressed limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}
	return decompressed, nil
}

func decompressZstd(body []byte, maxBytes int64) ([]byte, error) {
	reader, err := zstd.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decompressed, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > maxBytes {
		return nil, fmt.Errorf("%w: decompressed limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}
	return decompressed, nil
}

func decompressBrotli(body []byte, maxBytes int64) ([]byte, error) {
	reader := brotli.NewReader(bytes.NewReader(body))
	decompressed, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decompressed)) > maxBytes {
		return nil, fmt.Errorf("%w: decompressed limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}
	return decompressed, nil
}

