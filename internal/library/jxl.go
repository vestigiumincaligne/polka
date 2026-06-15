// JPEG XL cover support. Recent Flibusta dumps store covers in covers/ as
// JXL, which browsers barely render (Chrome/Edge dropped support, Firefox is
// behind a flag), so we decode it on the server and re-encode to JPEG. The
// gen2brain/jpegxl decoder runs on wazero (pure Go, no CGO) under the
// "nodynamic" build tag, so cross-compilation to every target is preserved.
package library

import (
	"bytes"
	"image/jpeg"

	"github.com/gen2brain/jpegxl"
)

// isJXL recognizes JPEG XL by signature: a bare codestream (FF 0A) or the
// ISO-BMFF container (0x0C "JXL \r\n\x87\n").
func isJXL(d []byte) bool {
	if len(d) >= 2 && d[0] == 0xFF && d[1] == 0x0A {
		return true
	}
	return len(d) >= 12 && bytes.Equal(d[:12],
		[]byte{0x00, 0x00, 0x00, 0x0C, 'J', 'X', 'L', ' ', 0x0D, 0x0A, 0x87, 0x0A})
}

// jxlToJPEG decodes JXL and re-encodes it as JPEG.
func jxlToJPEG(d []byte) ([]byte, string, error) {
	img, err := jpegxl.Decode(bytes.NewReader(d))
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

// normalizeCover converts a cover into something browsers understand: JXL is
// re-encoded to JPEG, everything else is returned as is. The result is cached
// higher up the stack (Library.covers), so the transcode happens once.
func normalizeCover(data []byte, mime string) ([]byte, string) {
	if isJXL(data) {
		if out, m, err := jxlToJPEG(data); err == nil {
			return out, m
		}
	}
	return data, mime
}
