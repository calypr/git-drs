package indexd_client

import "io"

type progressReadCloser struct {
	io.ReadCloser
	report func(int64)
}

func (p *progressReadCloser) Read(buf []byte) (int, error) {
	n, err := p.ReadCloser.Read(buf)
	if n > 0 && p.report != nil {
		p.report(int64(n))
	}
	return n, err
}
