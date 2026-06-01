package engine

import (
	"fmt"
	"io"
	"sync"
)

const defaultMaxResponseBytes = 16 << 20

var bodyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)
		return &b
	},
}

func readResponseBody(r io.Reader, limit int64) ([]byte, error) {
	capBytes := limit
	if capBytes == 0 {
		capBytes = defaultMaxResponseBytes
	}
	if capBytes < 0 {
		return io.ReadAll(r)
	}

	lr := io.LimitReader(r, capBytes+1)
	p := bodyBufPool.Get().(*[]byte)
	buf := *p
	defer func() {
		*p = buf[:0]
		bodyBufPool.Put(p)
	}()

	for {
		if len(buf) == cap(buf) {
			if int64(len(buf)) > capBytes {
				return nil, fmt.Errorf("httpc: response body exceeds %d bytes", capBytes)
			}
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := lr.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			if err == io.EOF {
				if int64(len(buf)) > capBytes {
					return nil, fmt.Errorf("httpc: response body exceeds %d bytes", capBytes)
				}
				out := make([]byte, len(buf))
				copy(out, buf)
				return out, nil
			}
			return nil, err
		}
	}
}
