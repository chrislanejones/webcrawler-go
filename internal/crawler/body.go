package crawler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const DefaultMaxBodyBytes int64 = 32 << 20

var errBodyTooLarge = errors.New("response body exceeds size limit")

// Limit decoded bytes, including chunked responses and automatically
// decompressed gzip. Never pass a truncated document to a parser.
func readResponseBody(resp *http.Response, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	tooLarge := fmt.Errorf("%w (%d bytes)", errBodyTooLarge, limit)
	if resp.ContentLength > limit {
		return nil, tooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) == limit {
		var extra [1]byte
		n, err := io.ReadFull(resp.Body, extra[:])
		if n > 0 {
			return nil, tooLarge
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
	}
	return body, nil
}
