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
	d := webtransport.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: t.skipVerify, //nolint:gosec
		},
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
