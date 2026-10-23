package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// acceptEncoding lists every coding the daemon may answer with. Compression is
// opt-in: the daemon only compresses when this header is present, and older
// daemons that do not know it simply answer with an identity body.
const acceptEncoding = "zstd, br, gzip"

// decompressResponse wraps the response body with the decoder selected by its
// Content-Encoding. The returned cleanup function releases decoder resources;
// the caller still owns resp.Body and must close it.
func decompressResponse(resp *http.Response) (io.Reader, func(), error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "", "identity":
		return resp.Body, func() {}, nil
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("gzip: %w", err)
		}
		return zr, func() { _ = zr.Close() }, nil
	case "br":
		// brotli.Reader has no Close; resp.Body is closed by the caller.
		return brotli.NewReader(resp.Body), func() {}, nil
	case "zstd":
		zr, err := zstd.NewReader(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("zstd: %w", err)
		}
		return zr, func() { zr.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported Content-Encoding %q", encoding)
	}
}
