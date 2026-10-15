package handlers

import (
	"bufio"
	"bytes"
	"errors"
	"io"

	"github.com/klauspost/compress/gzip"
	"github.com/mholt/archives"
)

// gzipFormat accepts non-gzip trailing data without discarding subsequent gzip
// members or hiding corruption within a member.
type gzipFormat struct{ archives.Gz }

func (gzipFormat) OpenReader(input io.Reader) (io.ReadCloser, error) {
	buffered := bufio.NewReader(input)
	reader, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, err
	}
	reader.Multistream(false)
	return &gzipMemberReader{input: buffered, reader: reader}, nil
}

type gzipMemberReader struct {
	input  *bufio.Reader
	reader *gzip.Reader
	done   bool
}

func (r *gzipMemberReader) Read(data []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	for {
		n, err := r.reader.Read(data)
		if !errors.Is(err, io.EOF) {
			return n, err
		}
		header, peekErr := r.input.Peek(2)
		if peekErr != nil && !errors.Is(peekErr, io.EOF) {
			return n, peekErr
		}
		if !bytes.Equal(header, []byte{0x1f, 0x8b}) {
			r.done = true
			return n, io.EOF
		}
		if err := r.reader.Reset(r.input); err != nil {
			return n, err
		}
		r.reader.Multistream(false)
		if n > 0 {
			return n, nil
		}
	}
}

func (r *gzipMemberReader) Close() error { return r.reader.Close() }
