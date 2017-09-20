package cachecontrol

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dc0d/tinykv"
)

//-----------------------------------------------------------------------------

const (
	cachecontrolHeaderServer = "Cache-Control"
	etagHeaderServer         = "ETag"
	etagHeaderClient         = "If-None-Match"
)

//-----------------------------------------------------------------------------

var kv = tinykv.New(tinykv.ExpirationInterval(time.Second * 30))

// DevelopmentMode checks for expired entries every time.Millisecond * 10
func DevelopmentMode() {
	kv = tinykv.New(tinykv.ExpirationInterval(time.Millisecond * 10))
}

//-----------------------------------------------------------------------------

type ccResponseWriter struct {
	rw     http.ResponseWriter
	rq     *http.Request
	status int

	options ccoptions
}

func (ccrw *ccResponseWriter) Header() http.Header { return ccrw.rw.Header() }

func (ccrw *ccResponseWriter) Write(b []byte) (int, error) {
	if ccrw.rq.Method == "GET" &&
		len(ccrw.rq.URL.Path) > 0 &&
		len(b) > 0 &&
		(ccrw.status == 0 || ccrw.status == http.StatusOK) {
		// search if md5 based etag	is already calculated
		etag, ok := kv.Get(ccrw.rq.URL.Path)
		// if not, calculate it
		if !ok {
			hash := md5.New()
			for header := range ccrw.rw.Header() {
				io.WriteString(hash, header)
			}
			io.WriteString(hash, string(b))
			var etagFormat = "\"%x\""
			if ccrw.options.isWeak {
				etagFormat = "W/" + etagFormat
			}
			etag = fmt.Sprintf(etagFormat, hash.Sum(nil))
			kv.CAS(ccrw.rq.URL.Path, etag, func(v interface{}, pe error) bool {
				if pe != tinykv.ErrNotFound {
					return false
				}
				return true
			},
				tinykv.ExpiresAfter(time.Second*time.Duration(ccrw.options.maxAge)))
		}

		// set cache control headers
		accessScope := "public"
		if ccrw.options.isPrivate {
			accessScope = "private"
		}
		ccrw.rw.Header().Set(cachecontrolHeaderServer,
			fmt.Sprintf(accessScope+", max-age=%d", ccrw.options.maxAge))
		ccrw.rw.Header().Set(etagHeaderServer, etag.(string))

		// if client sent If-None-Match then just answer with http.StatusNotModified/304
		// and do not send actual content
		if found, ok := ccrw.rq.Header[etagHeaderClient]; ok && found[0] == etag {
			ccrw.WriteHeader(http.StatusNotModified)
			b = nil
		}
	}

	return ccrw.rw.Write(b)
}

func (ccrw *ccResponseWriter) WriteHeader(st int) {
	ccrw.rw.WriteHeader(st)
	ccrw.status = st
}

//-----------------------------------------------------------------------------
// options

type ccoptions struct {
	maxAge    int
	isPrivate bool
	isWeak    bool
}

// Option .
type Option func(ccoptions) ccoptions

// MaxAge sets "max-age" in "Cache-Control" header, in seconds
func MaxAge(maxAge int) Option {
	return func(cco ccoptions) ccoptions {
		cco.maxAge = maxAge
		return cco
	}
}

// IsPrivate marks "Cache-Control" as private
func IsPrivate(isPrivate bool) Option {
	return func(cco ccoptions) ccoptions {
		cco.isPrivate = isPrivate
		return cco
	}
}

// IsWeak sets if the generated ETag should be a weak one
func IsWeak(isWeak bool) Option {
	return func(cco ccoptions) ccoptions {
		cco.isWeak = isWeak
		return cco
	}
}

//-----------------------------------------------------------------------------

// CacheControl is a middleware for adding ETag and Cache-Control headers,
// and checks If-None-Match client header
func CacheControl(options ...Option) func(http.Handler) http.Handler {
	var cco ccoptions
	for _, opt := range options {
		cco = opt(cco)
	}

	if cco.maxAge <= 0 {
		cco.maxAge = int((time.Hour * 24 * 28).Seconds())
	}

	return func(next http.Handler) http.Handler {
		h := func(rw http.ResponseWriter, rq *http.Request) {
			ccrw := &ccResponseWriter{
				rw:      rw,
				rq:      rq,
				status:  http.StatusOK,
				options: cco,
			}
			next.ServeHTTP(ccrw, rq)
		}
		return http.HandlerFunc(h)
	}
}

//-----------------------------------------------------------------------------
