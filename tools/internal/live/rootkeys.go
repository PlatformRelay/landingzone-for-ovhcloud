package live

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RootKeys are the account root's application key, application secret and consumer key (AK, AS,
// CK), typed at a no-echo prompt under --fresh-account and held only in memory (FR-010): never in
// a file, an argument, an environment variable or a child process. Every formatting of them
// prints a placeholder.
type RootKeys struct{ ak, as, ck string }

func (RootKeys) String() string   { return "[root keys]" }
func (RootKeys) GoString() string { return "[root keys]" }

// secrets are the three values, for the run's Redactor.
func (k RootKeys) secrets() []string { return []string{k.ak, k.as, k.ck} }

// rootClient signs each request in process (research R19; kb guide
// manage-and-operate/api/first-steps.mdx "First API Usage": X-Ovh-Signature =
// "$1$" + SHA1_HEX(AS+"+"+CK+"+"+METHOD+"+"+QUERY+"+"+BODY+"+"+TSTAMP), QUERY the full URL). The
// application secret never leaves the process; AK and CK travel only in their own headers.
type rootClient struct {
	api  API
	keys RootKeys
	// skew is the API's clock minus ours, from GET /auth/time (unauthenticated, kb/api/v1/auth.json),
	// read once before the first signed call so a drifting workstation clock does not void the
	// signature; nil until read.
	skew *time.Duration
}

func newRootClient(a API, k RootKeys) *rootClient { return &rootClient{api: a, keys: k} }

// timestamp is the API's current time in Unix seconds.
func (c *rootClient) timestamp(ctx context.Context) (string, error) {
	if c.skew == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api.BaseURL+"/auth/time", nil)
		if err != nil {
			return "", fmt.Errorf("GET /auth/time: bad request")
		}
		var now int64
		if err := send(c.api, req, "GET /auth/time", &now); err != nil {
			return "", err
		}
		d := time.Unix(now, 0).Sub(time.Now())
		c.skew = &d
	}
	return strconv.FormatInt(time.Now().Add(*c.skew).Unix(), 10), nil
}

// call sends one signed request; in (if any) is the JSON body, out receives a 2xx answer.
func (c *rootClient) call(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%s %s: bad request body", method, path)
		}
		body = b
	}
	ts, err := c.timestamp(ctx)
	if err != nil {
		return err
	}
	u := apiBase(c.api, path) + path
	sum := sha1.Sum([]byte(c.keys.as + "+" + c.keys.ck + "+" + method + "+" + u + "+" + string(body) + "+" + ts))
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s %s: bad request", method, path)
	}
	req.Header.Set("X-Ovh-Application", c.keys.ak)
	req.Header.Set("X-Ovh-Consumer", c.keys.ck)
	req.Header.Set("X-Ovh-Timestamp", ts)
	req.Header.Set("X-Ovh-Signature", "$1$"+hex.EncodeToString(sum[:]))
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return send(c.api, req, method+" "+path, out)
}
