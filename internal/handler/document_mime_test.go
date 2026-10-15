package handler

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectDocumentMIME(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"standard", []byte("%PDF-1.7\n"), "application/pdf"},
		{"bom", []byte("\xef\xbb\xbf%PDF-1.7\n"), "application/pdf"},
		{"binary prefix", append(bytes.Repeat([]byte{0}, 512), []byte("%PDF-2.0\n")...), "application/pdf"},
		{"outside header window", append(bytes.Repeat([]byte{0}, 1024), []byte("%PDF-1.7\n")...), "application/octet-stream"},
		{"invalid version", []byte("\x00%PDF-nope\n"), "application/octet-stream"},
		{"html with embedded marker", []byte("<html><body>%PDF-1.7\n</body></html>"), "text/html; charset=utf-8"},
		{"binary", []byte{0, 1, 2, 3}, "application/octet-stream"},
		{"text", []byte("This is not a PDF"), "text/plain; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, detectDocumentMIME(tc.data)) })
	}
}
