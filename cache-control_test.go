package cachecontrol

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/stretchr/testify/assert"
)

var _ http.ResponseWriter = &ccResponseWriter{}

// files = map[url-path]content
func createMux(files map[string]string, options ...Option) *chi.Mux {
	rt := chi.NewRouter()
	rt.Route("/assets/*", func(rt chi.Router) {
		rt.Use(CacheControl(options...))
		rt.Get("/*", func(rw http.ResponseWriter, rq *http.Request) {
			f, ok := files[rq.URL.Path]
			if !ok {
				rw.WriteHeader(http.StatusNotFound)
				rw.Write([]byte("404 for test: " + rq.URL.Path))
				return
			}
			rw.Write([]byte(f))
		})
	})
	return rt
}

const goInternalETagHeader = "Etag" // it's case-insensitive; but why the change?

func TestETag1(t *testing.T) {
	DevelopmentMode()

	assert := assert.New(t)

	files := map[string]string{
		"/assets/js/app.js":              "var v = 66",
		"/assets/js/vue.js":              "var vue = 66",
		"/assets/css/framework.css":      "body { direction: rtl; }",
		"/assets/css/customizations.css": "body { x: 10; }",
		"/assets/css/lang.css":           "body { y: 10; }",
		"/assets/css/app.css":            "body { z: 10; }",
	}

	mux := createMux(files)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for k, vf := range files {
		var u bytes.Buffer
		u.WriteString(string(srv.URL))
		u.WriteString(k)

		// 1
		res, err := http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err := ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("public, max-age=2419200", res.Header[cachecontrolHeaderServer][0])
		foundETag := res.Header[goInternalETagHeader][0]
		assert.Equal(http.StatusOK, res.StatusCode)

		// 2
		req, err := http.NewRequest("GET", u.String(), nil)
		assert.NoError(err)
		req.Header.Set(etagHeaderClient, foundETag)
		res, err = (&http.Client{}).Do(req)
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err = ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("public, max-age=2419200", res.Header[cachecontrolHeaderServer][0])
		assert.Equal(foundETag, res.Header[goInternalETagHeader][0])
		assert.True(len(b) == 0)
		assert.Equal(http.StatusNotModified, res.StatusCode)

		// 3
		files[k] = "N/A"
		res, err = http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err = ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("public, max-age=2419200", res.Header[cachecontrolHeaderServer][0])
		assert.Equal(foundETag, res.Header[goInternalETagHeader][0])
		assert.True(!bytes.Contains(b, []byte(vf)))
	}
}

func TestETag2(t *testing.T) {
	DevelopmentMode()
	assert := assert.New(t)

	files := map[string]string{
		"/assets/js/app.js":              "var v = 66",
		"/assets/js/vue.js":              "var vue = 66",
		"/assets/css/framework.css":      "body { direction: rtl; }",
		"/assets/css/customizations.css": "body { x: 10; }",
		"/assets/css/lang.css":           "body { y: 10; }",
		"/assets/css/app.css":            "body { z: 10; }",
	}

	mux := createMux(files, MaxAge(1), IsPrivate(true), IsWeak(true))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for k, vf := range files {
		var u bytes.Buffer
		u.WriteString(string(srv.URL))
		u.WriteString(k)

		// 1
		res, err := http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err := ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("private, max-age=1", res.Header[cachecontrolHeaderServer][0])
		foundETag := res.Header[goInternalETagHeader][0]
		assert.True(strings.HasPrefix(foundETag, "W/"), foundETag)

		// time.Sleep(time.Millisecond * 1100)

		// 2
		res, err = http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err = ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("private, max-age=1", res.Header[cachecontrolHeaderServer][0])
		assert.Equal(foundETag, res.Header[goInternalETagHeader][0])
		assert.True(bytes.Contains(b, []byte(vf)))

		// 3
		files[k] = "N/A"
		res, err = http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err = ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("private, max-age=1", res.Header[cachecontrolHeaderServer][0])
		assert.Equal(foundETag, res.Header[goInternalETagHeader][0])
		assert.True(!bytes.Contains(b, []byte(vf)))
	}
}
