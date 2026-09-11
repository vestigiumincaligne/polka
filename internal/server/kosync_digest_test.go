package server

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"math/rand"
	"testing"
)

// referenceDigest is an independent transcription of KOReader's
// util.partialMD5: hash 1024-byte reads at offsets 0, 1024, 4096, …
// (1024·4ⁱ), stopping at the first offset at or past the end of file.
func referenceDigest(data []byte) string {
	h := md5.New()
	offsets := []int64{0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}
	for _, off := range offsets {
		if off >= int64(len(data)) {
			break
		}
		end := off + 1024
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		h.Write(data[off:end])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestKosyncSampleOffsets(t *testing.T) {
	want := []int64{0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864, 268435456, 1073741824}
	for i, w := range want {
		if got := kosyncSampleOffset(i); got != w {
			t.Errorf("offset[%d] = %d, want %d", i, got, w)
		}
	}
}

func TestDigestTee(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	sizes := []int{1, 500, 1023, 1024, 1025, 2048, 5000, 70_000, 300_000, 2_000_000}
	for _, size := range sizes {
		data := make([]byte, size)
		rnd.Read(data)
		want := referenceDigest(data)
		// Stream in awkward chunk sizes to exercise the range capture.
		for _, chunk := range []int{1, 7, 333, 4096, size} {
			tee := &digestTee{}
			for off := 0; off < size; off += chunk {
				end := off + chunk
				if end > size {
					end = size
				}
				if _, err := tee.Write(data[off:end]); err != nil {
					t.Fatal(err)
				}
			}
			if got := tee.digest(); got != want {
				t.Errorf("size %d chunk %d: digest %s, want %s", size, chunk, got, want)
			}
		}
	}
	// The digest of a streamed copy matches too (as used in the handler).
	data := make([]byte, 123_456)
	rnd.Read(data)
	tee := &digestTee{}
	src := io.LimitReader(rand.New(rand.NewSource(9)), 0) // unused, keep io import honest
	_ = src
	n, err := io.Copy(io.MultiWriter(io.Discard, tee), readerOf(data))
	if err != nil || n != int64(len(data)) {
		t.Fatal(err, n)
	}
	if tee.digest() != referenceDigest(data) {
		t.Error("copy digest mismatch")
	}
}

type sliceReader struct {
	data []byte
	pos  int
}

func readerOf(b []byte) io.Reader { return &sliceReader{data: b} }

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	// Deliberately small, uneven reads.
	n := copy(p[:min(len(p), 777)], r.data[r.pos:])
	r.pos += n
	return n, nil
}
