package cachecontrol

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/stretchr/testify/assert"
)

var fs = http.Dir(`./assets`)

func createMux(options ...Option) *chi.Mux {
	rt := chi.NewRouter()
	rt.Route("/assets/*", func(rt chi.Router) {
		options = append(options, StripPrefix("/assets"))
		rt.Use(CacheControl(fs, options...))
		rt.Get("/*", http.StripPrefix("/assets", http.FileServer(fs)).ServeHTTP)
	})

	return rt
}

const goInternalETagHeader = "Etag" // it's case-insensitive; but why the change?

// etags gets calculated on first call
func TestETag1WarmUp(t *testing.T) {
	assert := assert.New(t)

	files := []string{
		"/assets/js/app.js",
		"/assets/vue/vue.min.js",
		"/assets/axios/axios.min.js",
		"/assets/bulma/css/bulma.rtl.css",
	}

	mux := createMux()

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, vf := range files {
		var u bytes.Buffer
		u.WriteString(string(srv.URL))
		u.WriteString(vf)

		// 1
		res, err := http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}
	}

WAIT_FOR_ETAG:
	for _, vf := range files {
		_, ok := kv.Get(vf)
		if !ok {
			time.Sleep(time.Millisecond * 100)
			goto WAIT_FOR_ETAG
		}
	}
}

func TestETag1(t *testing.T) {
	assert := assert.New(t)

	files := []string{
		"/assets/js/app.js",
		"/assets/vue/vue.min.js",
		"/assets/axios/axios.min.js",
		"/assets/bulma/css/bulma.rtl.css",
	}

	mux := createMux()

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, vf := range files {
		var u bytes.Buffer
		u.WriteString(string(srv.URL))
		u.WriteString(vf)

		// 1
		res, err := http.Get(u.String())
		assert.NoError(err)
		if res != nil {
			defer res.Body.Close()
		}

		b, err := ioutil.ReadAll(res.Body)
		assert.NoError(err)
		assert.Equal("public, max-age=2419200", res.Header[cacheControlServer][0])
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
		assert.Equal("public, max-age=2419200", res.Header[cacheControlServer][0])
		assert.Equal(foundETag, res.Header[goInternalETagHeader][0])
		assert.True(len(b) == 0)
		assert.Equal(http.StatusNotModified, res.StatusCode)
	}
}
