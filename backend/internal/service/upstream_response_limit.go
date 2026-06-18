package service

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"

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

	// Handle Content-Encoding as per HTTP spec
	encoding := headers.Get("Content-Encoding")
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
	case "":
		// No encoding, return as-is
		return body, nil
	default:
		// Unknown encoding — don't try to decompress, let caller handle
		return body, nil
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

