package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	compressgzip "github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	logContext "github.com/trufflesecurity/trufflehog/v3/pkg/context"
	"github.com/trufflesecurity/trufflehog/v3/pkg/sources"
)

func TestGzipTrailingData(t *testing.T) {
	compress := func(data []byte) []byte {
		t.Helper()
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		_, err := writer.Write(data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		return buffer.Bytes()
	}
	payload := []byte("synthetic-secret-value-123456\n")
	first := compress(payload)
	second := compress([]byte("second synthetic value\n"))
	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "content.txt", Mode: 0600, Size: int64(len(payload))}))
	_, err := tarWriter.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())

	for _, tc := range []struct {
		name  string
		input []byte
		want  []byte
	}{
		{"single member", first, payload},
		{"trailing bytes", append(append([]byte{}, first...), []byte("non-gzip trailer")...), payload},
		{"multiple members", append(append([]byte{}, first...), second...), append(append([]byte{}, payload...), []byte("second synthetic value\n")...)},
		{"multiple members and trailer", append(append(append([]byte{}, first...), second...), []byte("trailer")...), append(append([]byte{}, payload...), []byte("second synthetic value\n")...)},
		{"tar and trailer", append(compress(archive.Bytes()), []byte("trailer")...), payload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks := make(chan *sources.Chunk, 2)
			reporter := sources.ChanReporter{Ch: chunks}
			require.NoError(t, HandleFile(logContext.Background(), bytes.NewReader(tc.input), &sources.Chunk{}, reporter))
			var got []byte
			for len(reporter.Ch) > 0 {
				got = append(got, (<-chunks).Data...)
			}
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("checksum failures are not ignored", func(t *testing.T) {
		corrupt := append([]byte{}, first...)
		corrupt[len(corrupt)-8] ^= 1
		reader, err := (gzipFormat{}).OpenReader(bytes.NewReader(corrupt))
		require.NoError(t, err)
		defer func() { require.NoError(t, reader.Close()) }()
		_, err = io.ReadAll(reader)
		require.ErrorIs(t, err, compressgzip.ErrChecksum)
	})
	t.Run("malformed next member is not a trailer", func(t *testing.T) {
		input := append(append([]byte{}, first...), 0x1f, 0x8b, 0, 0)
		reader, err := (gzipFormat{}).OpenReader(bytes.NewReader(input))
		require.NoError(t, err)
		defer func() { require.NoError(t, reader.Close()) }()
		_, err = io.ReadAll(reader)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}
