package blocklist

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// blocklistURL is the canonical StevenBlack unified hosts blocklist.
const blocklistURL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"

// maxBodyBytes is the maximum number of bytes read from the blocklist response body.
// The StevenBlack file is ~800KB; 10MB gives ample headroom while preventing OOM from
// a malicious or misconfigured server.
const maxBodyBytes = 10 * 1024 * 1024 // 10 MB

// download fetches the blocklist from blocklistURL using the provided HTTP client.
// Returns the raw body bytes or a wrapped error.
func download(ctx context.Context, client *http.Client) ([]byte, error) {
	return downloadFrom(ctx, client, blocklistURL)
}

// downloadFrom fetches the blocklist from the given URL. Extracted for testability.
func downloadFrom(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("blocklist: download: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("blocklist: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blocklist: download: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("blocklist: download: %w", err)
	}
	return data, nil
}
