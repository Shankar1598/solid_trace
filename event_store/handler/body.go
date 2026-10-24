package handler

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gofiber/fiber/v2"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
	"github.com/solidtrace/event_store/ingest"
)

// The caps on an ingest request's body. A body over its route's cap as sent,
// or over decodedBodyLimit once decoded, is refused with 413 and nothing in
// it is stored.
const (
	// eventBodyLimit caps the body of the Event routes, as sent. It is Fiber's
	// default, which every ingest route had before the Log route.
	eventBodyLimit = 4 << 20
	// logBodyLimit caps the body of the Log route, as sent. It is also the
	// ingest app's limit, so the server reads no larger body for any route.
	logBodyLimit = 10 << 20
	// decodedBodyLimit caps every ingest body once decoded. Decoding stops at
	// it rather than inflating the whole body.
	decodedBodyLimit = 50 << 20
)

var (
	errBodyTooLarge        = errors.New("request body too large")
	errUnsupportedEncoding = errors.New("unsupported Content-Encoding")
	errUndecodableBody     = errors.New("request body does not decode as its Content-Encoding")
)

// requestBody returns the request's body, to be decoded from its
// Content-Encoding (identity, gzip, deflate, br or zstd) only once the request
// holds a waiting place: an overloaded ingest turns it away before spending
// memory on decoding. A body over limit as sent is errBodyTooLarge, and an
// unknown coding errUnsupportedEncoding, at once; one over decodedBodyLimit
// once decoded is errBodyTooLarge from the decode.
//
// Fiber's c.Body() is not used: it inflates gzip, deflate and br with no cap
// and passes zstd through undecoded.
func requestBody(c *fiber.Ctx, limit int) (ingest.Body, error) {
	raw := c.Request().Body()
	if len(raw) > limit {
		return ingest.Body{}, errBodyTooLarge
	}
	coding, err := contentCoding(c.Get(fiber.HeaderContentEncoding))
	if err != nil {
		return ingest.Body{}, err
	}
	return ingest.EncodedBody(len(raw), func() ([]byte, error) { return decodeBody(raw, coding) }), nil
}

// decodeBody decodes a body sent in one content coding, "" for identity.
func decodeBody(raw []byte, coding string) ([]byte, error) {
	var decoder io.Reader
	switch coding {
	case "":
		return raw, nil
	case "gzip", "x-gzip":
		r, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, errUndecodableBody
		}
		decoder = r
	case "deflate":
		// HTTP's deflate is the zlib format (RFC 9110 section 8.4.1.2).
		r, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, errUndecodableBody
		}
		decoder = r
	case "br":
		decoder = brotli.NewReader(bytes.NewReader(raw))
	case "zstd":
		// Synchronous, and refusing a window larger than the decoded cap.
		r, err := zstd.NewReader(bytes.NewReader(raw), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(decodedBodyLimit))
		if err != nil {
			return nil, errUndecodableBody
		}
		defer r.Close()
		decoder = r
	}
	return readDecoded(decoder)
}

// contentCoding returns the one content coding a Content-Encoding header
// names, lower-cased, or "" for identity. Only one coding is decoded: a body
// encoded twice is refused rather than decoded layer by layer.
func contentCoding(header string) (string, error) {
	var coding string
	for _, name := range strings.Split(header, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "", "identity":
			continue
		case "gzip", "x-gzip", "deflate", "br", "zstd":
		default:
			return "", errUnsupportedEncoding
		}
		if coding != "" {
			return "", errUnsupportedEncoding
		}
		coding = name
	}
	return coding, nil
}

// readDecoded reads a decoder to its end, or to just past decodedBodyLimit.
func readDecoded(decoder io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(decoder, decodedBodyLimit+1))
	switch {
	case errors.Is(err, zstd.ErrDecoderSizeExceeded):
		return nil, errBodyTooLarge
	case err != nil:
		return nil, errUndecodableBody
	case len(body) > decodedBodyLimit:
		return nil, errBodyTooLarge
	}
	return body, nil
}

// isBodyError reports whether err is a request body's own: too large, in an
// unsupported encoding, or not decoding as its encoding.
func isBodyError(err error) bool {
	return errors.Is(err, errBodyTooLarge) || errors.Is(err, errUnsupportedEncoding) || errors.Is(err, errUndecodableBody)
}

// renderBodyError answers a body that requestBody or its decode refused.
func renderBodyError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errBodyTooLarge):
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "Request body too large"})
	case errors.Is(err, errUnsupportedEncoding):
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{"error": "Unsupported Content-Encoding"})
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Request body does not decode as its Content-Encoding"})
	}
}
