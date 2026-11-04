package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// UploadCreativeArtifact streams a client-local file to the daemon. It does not
// JSON/base64 encode or buffer media. Ordinary JSON requests keep their existing
// short deadlines; this bounded transfer also honors caller cancellation.
func (c *Client) UploadCreativeArtifact(ctx context.Context, path, filename, mediaType string, body io.Reader, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("build artifact URL: %w", err)
	}
	q := u.Query()
	q.Set("filename", filename)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return err
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", mediaType)
	if sid := os.Getenv("ROCKET_SESSION_ID"); sid != "" {
		req.Header.Set("X-Rocket-Session", sid)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("upload creative artifact: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return fmt.Errorf("read publication response: %w", err)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("publication response exceeds 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode publication response: %w", err)
		}
	}
	return nil
}
