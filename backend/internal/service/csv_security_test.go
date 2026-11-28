package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCSVFormulaEscapeRoundTrip(t *testing.T) {
	for _, v := range []string{"=1+1", "+49 170", "-5", "@x", "'=quoted", "'plain", "plain", "", "a=b"} {
		e := escapeCSVFormula(v)
		require.False(t, e != "" && e[0] != '\'' && isCSVFormula(e), v)
		require.Equal(t, v, unescapeCSVFormula(e), v)
	}
	require.Equal(t, "'=1+1", escapeCSVFormula("=1+1"))
}

func TestImageDecodeRejectsHugeImages(t *testing.T) {
	// PNG header claiming 30000 x 30000 pixels; only the header may be read.
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x75\x30\x00\x00\x75\x30\x08\x02\x00\x00\x00")
	png = binary.BigEndian.AppendUint32(png, crc32.ChecksumIEEE(png[12:]))
	_, err := EncodeWebPFromImage(bytes.NewReader(png))
	require.ErrorContains(t, err, "too large")
}
