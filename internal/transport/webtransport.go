package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"github.com/quic-go/webtransport-go"
)

type wtTransport struct {
	url        string
	skipVerify bool
}

func (t *wtTransport) Dial(ctx context.Context) (io.ReadWriteCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	/* #nosec G402 */
	tlsCfg := &tls.Config{
		InsecureSkipVerify: t.skipVerify,
	}
	if t.skipVerify {
		tlsCfg.VerifyConnection = verifySelfSigned
	}

	d := webtransport.Dialer{
		TLSClientConfig: tlsCfg,
	}
	_, session, err := d.Dial(ctx, t.url, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("webtransport dial %s: %w", t.url, err)
	}
	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("webtransport open stream %s: %w", t.url, err)
	}
	return stream, nil
}
