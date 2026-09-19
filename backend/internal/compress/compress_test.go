package compress

import (
	"bytes"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte("INSERT INTO t VALUES (1, 'hello world');\n"), 20000)
	for _, kind := range []string{Zstd, Gzip, None} {
		t.Run(kind, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(kind, &buf)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if kind != None && buf.Len() >= len(data)/10 {
				t.Errorf("%s compressed poorly: %d -> %d", kind, len(data), buf.Len())
			}
			r, err := NewReader(kind, &buf)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) {
				t.Fatal("round trip mismatch")
			}
		})
	}
}

func TestUnknownKind(t *testing.T) {
	if _, err := NewWriter("brotli", io.Discard); err == nil {
		t.Fatal("expected error")
	}
	if Valid("lz4") || !Valid(Zstd) {
		t.Fatal("Valid is wrong")
	}
}

func TestExtensions(t *testing.T) {
	if Extension(Zstd) != ".zst" || Extension(Gzip) != ".gz" || Extension(None) != "" {
		t.Fatal("unexpected extensions")
	}
}
