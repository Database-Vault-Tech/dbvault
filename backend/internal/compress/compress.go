// Package compress wraps streaming zstd and gzip codecs.
package compress

import (
	"fmt"
	"io"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

const (
	Zstd = "zstd"
	Gzip = "gzip"
	None = "none"
)

// Valid reports whether kind is a supported compression algorithm.
func Valid(kind string) bool { return kind == Zstd || kind == Gzip || kind == None }

// Extension returns the file extension (including the dot) for kind.
func Extension(kind string) string {
	switch kind {
	case Zstd:
		return ".zst"
	case Gzip:
		return ".gz"
	default:
		return ""
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// NewWriter returns a streaming compressor writing into w. Close flushes the
// final frame but does not close w.
func NewWriter(kind string, w io.Writer) (io.WriteCloser, error) {
	switch kind {
	case Zstd:
		// SpeedDefault (level 3) is the usual sweet spot for database dumps.
		// The window is capped so memory stays bounded for arbitrarily large inputs.
		return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(2), zstd.WithWindowSize(8<<20))
	case Gzip:
		return gzip.NewWriterLevel(w, gzip.DefaultCompression)
	case None:
		return nopWriteCloser{w}, nil
	default:
		return nil, fmt.Errorf("unsupported compression %q", kind)
	}
}

// NewReader returns a streaming decompressor reading from r.
func NewReader(kind string, r io.Reader) (io.ReadCloser, error) {
	switch kind {
	case Zstd:
		d, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(128<<20))
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	case Gzip:
		return gzip.NewReader(r)
	case None:
		return io.NopCloser(r), nil
	default:
		return nil, fmt.Errorf("unsupported compression %q", kind)
	}
}
