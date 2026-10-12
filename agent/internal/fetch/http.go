package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

func (f *Fetcher) httpClient() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	return &http.Client{Timeout: 0}
}

func (f *Fetcher) checkServerURL(u *url.URL) (string, error) {
	if f.serverBase == "" {
		return "", fmt.Errorf("fetch: https assets require server base configured")
	}
	base, err := url.Parse(f.serverBase)
	if err != nil {
		return "", err
	}
	if u.Host != base.Host || u.Scheme != base.Scheme {
		return "", fmt.Errorf("fetch: https uri %s not under server base %s", u, f.serverBase)
	}
	return base.String(), nil
}

func (f *Fetcher) fetchHTTPS(ctx context.Context, u *url.URL, w io.Writer) error {
	if _, err := f.checkServerURL(u); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if tok := f.sessionToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := f.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("fetch: https get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch: https get %s: status %d", u, resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (f *Fetcher) UploadArtifact(ctx context.Context, jobID, localPath, relPath string) error {
	if f.serverBase == "" {
		return fmt.Errorf("fetch: artifact upload requires server base configured")
	}
	base, err := url.Parse(f.serverBase)
	if err != nil {
		return err
	}
	u := *base
	u.Path = path.Join(base.Path, "artifacts", jobID, relPath)

	fh, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), fh)
	if err != nil {
		return err
	}
	req.ContentLength = st.Size()
	if tok := f.sessionToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := f.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("fetch: upload %s: %w", relPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fetch: upload %s: status %d", relPath, resp.StatusCode)
	}
	return nil
}

func WsToHTTPBase(wsURL string) string {
	if strings.HasPrefix(wsURL, "wss://") {
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	}
	if strings.HasPrefix(wsURL, "ws://") {
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	}
	return wsURL
}
