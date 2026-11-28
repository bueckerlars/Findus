package handler

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestAssetHandler(t *testing.T) {
	h := assetHandler(fstest.MapFS{
		"app.js":   {Data: []byte("console.log(1)")},
		"logo.png": {Data: []byte("png")},
	})
	get := func(path, encoding string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.Path = path
		r.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := get("app.js", "gzip, br")
	require.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	require.Contains(t, w.Header().Get("Content-Type"), "javascript")
	require.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	require.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	zr, err := gzip.NewReader(w.Body)
	require.NoError(t, err)
	b, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.Equal(t, "console.log(1)", string(b))

	w = get("app.js", "")
	require.Empty(t, w.Header().Get("Content-Encoding"))
	require.Equal(t, "console.log(1)", w.Body.String())

	w = get("logo.png", "gzip")
	require.Empty(t, w.Header().Get("Content-Encoding"))
	require.Contains(t, w.Header().Get("Cache-Control"), "immutable")

	w = get("gone.js", "gzip")
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Empty(t, w.Header().Get("Cache-Control"))
}
