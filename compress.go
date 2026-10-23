package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// minCompressBytes is the smallest JSON body worth compressing. Below it the
// framing overhead outweighs the savings.
const minCompressBytes = 1024

// supportedEncodings is ordered by the daemon's preference: zstd compresses
// fastest at a good ratio, brotli produces the smallest bodies, gzip is the
// most widely supported.
var supportedEncodings = []string{"zstd", "br", "gzip"}

var errNotAcceptable = errors.New("no acceptable content encoding")

// acceptEncodingHeader joins repeated Accept-Encoding header lines, as allowed
// by RFC 9110.
func acceptEncodingHeader(r *http.Request) string {
	return strings.Join(r.Header.Values("Accept-Encoding"), ",")
}

// negotiateEncoding interprets an Accept-Encoding header and returns the best
// supported coding ("" means identity) plus whether identity is allowed.
//
// The rules follow RFC 9110 section 12.5.3: q-values select the preferred
// coding, "*" covers codings that are not listed, and identity is acceptable
// unless it is explicitly excluded. An empty header means the client did not
// ask for compression, so the caller must send identity.
func negotiateEncoding(header string) (coding string, identityOK bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", true
	}

	quality := map[string]float64{}
	for _, part := range strings.Split(header, ",") {
		name, q := parseEncodingToken(part)
		if name == "" {
			continue
		}
		quality[name] = q
	}

	wildcard, hasWildcard := quality["*"]
	identityQ, hasIdentity := quality["identity"]
	identityOK = true
	if hasIdentity {
		identityOK = identityQ > 0
	} else if hasWildcard {
		identityOK = wildcard > 0
	}

	best := ""
	bestQ := 0.0
	for _, candidate := range supportedEncodings {
		q, ok := quality[candidate]
		if !ok && candidate == "gzip" {
			// "x-gzip" is the historical alias of gzip.
			q, ok = quality["x-gzip"]
		}
		if !ok {
			if !hasWildcard {
				continue
			}
			q = wildcard
		}
		if q <= 0 {
			continue
		}
		// Iterating in preference order and using a strict compare keeps the
		// daemon's order for equal q-values.
		if q > bestQ {
			best, bestQ = candidate, q
		}
	}

	if best == "" {
		return "", identityOK
	}
	if hasIdentity && identityQ > bestQ {
		// The client explicitly prefers identity over every coding it accepts.
		return "", true
	}
	return best, identityOK
}

// parseEncodingToken parses "gzip", "br;q=0.8" or "zstd; q=1.0". Unknown
// parameters and malformed q-values are ignored (q defaults to 1).
func parseEncodingToken(token string) (name string, q float64) {
	parts := strings.Split(token, ";")
	name = strings.ToLower(strings.TrimSpace(parts[0]))
	q = 1
	for _, param := range parts[1:] {
		eq := strings.IndexByte(param, '=')
		if eq < 0 || !strings.EqualFold(strings.TrimSpace(param[:eq]), "q") {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(param[eq+1:]), 64)
		if err != nil {
			continue
		}
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		q = v
	}
	return name, q
}

// zstdEncoders pools encoders because zstd.Encoder allocates a sizeable state.
// EncodeAll is not safe on a shared encoder, so one encoder per concurrent use.
var zstdEncoders = sync.Pool{
	New: func() any {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			panic(err) // static configuration; cannot fail
		}
		return enc
	},
}

// compressBody encodes body with coding, which must be one of
// supportedEncodings.
func compressBody(coding string, body []byte) ([]byte, error) {
	switch coding {
	case "zstd":
		enc := zstdEncoders.Get().(*zstd.Encoder)
		defer zstdEncoders.Put(enc)
		return enc.EncodeAll(body, nil), nil
	case "br":
		var buf bytes.Buffer
		w := brotli.NewWriterLevel(&buf, brotli.DefaultCompression)
		if _, err := w.Write(body); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "gzip":
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(body); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, errNotAcceptable
	}
}

// writeJSON serialises v and, when the request allows it, compresses the body.
// Compression is opt-in: a request without Accept-Encoding always receives an
// identity body, which keeps old clients working. Responses below
// minCompressBytes, or bodies that do not actually shrink, also stay identity.
func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json.Encoder appends a newline; keep the wire format familiar.
	body = append(body, '\n')

	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Type", "application/json")

	coding, identityOK := negotiateEncoding(acceptEncodingHeader(r))
	if coding == "" && !identityOK {
		http.Error(w, errNotAcceptable.Error(), http.StatusNotAcceptable)
		return
	}
	if coding != "" && len(body) >= minCompressBytes {
		compressed, err := compressBody(coding, body)
		if err != nil {
			log.Printf("compress %s response: %v", coding, err)
		} else if len(compressed) < len(body) {
			w.Header().Set("Content-Encoding", coding)
			w.Header().Set("Content-Length", strconv.Itoa(len(compressed)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(compressed)
			return
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// acceptEncodingMiddleware rejects a request that accepts neither a supported
// coding nor identity before any RCON work is done. The response writer still
// performs the actual negotiation.
func acceptEncodingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if coding, identityOK := negotiateEncoding(acceptEncodingHeader(r)); coding == "" && !identityOK {
			w.Header().Set("Vary", "Accept-Encoding")
			http.Error(w, errNotAcceptable.Error(), http.StatusNotAcceptable)
			return
		}
		next.ServeHTTP(w, r)
	})
}
