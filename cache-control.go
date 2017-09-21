package cachecontrol

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dc0d/tinykv"
)

//-----------------------------------------------------------------------------

const (
	cacheControlServer = "Cache-Control"
	etagHeaderServer   = "ETag"
	etagHeaderClient   = "If-None-Match"

	calcPrefix = "CALC-"
)

var (
	kv            = tinykv.New(tinykv.ExpirationInterval(time.Second * 30))
	devMode       = false
	defaultETag   = fmt.Sprintf("%d", time.Now().UTC().Unix())
	defaultMaxAge = int((time.Hour * 24 * 28).Seconds())
)

// DevelopmentMode checks for expired entries every time.Millisecond * 10
func DevelopmentMode() {
	kv = tinykv.New(tinykv.ExpirationInterval(time.Millisecond * 10))
	devMode = true
}

//-----------------------------------------------------------------------------
// options

type ccoptions struct {
	maxAge      int
	isPrivate   bool
	isWeak      bool
	stripPrefix string
	fs          http.FileSystem
}

// Option .
type Option func(ccoptions) ccoptions

// StripPrefix .
func StripPrefix(stripPrefix string) Option {
	return func(cco ccoptions) ccoptions {
		cco.stripPrefix = stripPrefix
		return cco
	}
}

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

func calculateETag(urlPath string, fs http.FileSystem, cco ccoptions) error {
	if fs == nil || len(urlPath) == 0 {
		return nil
	}

	// check if already calculating
	calcKey := calcPrefix + urlPath
	_, ok := kv.Get(calcKey)
	if ok {
		// already calculating
		return nil
	}
	if err := kv.CAS(calcKey, struct{}{}, func(v interface{}, pe error) bool {
		if pe != tinykv.ErrNotFound {
			return false
		}
		return true
	}); err != nil {
		// already calculating
		return nil
	}
	defer kv.Delete(calcKey)

	// get file
	var f http.File
	var err error
	if f, err = fs.Open(strings.TrimPrefix(urlPath, cco.stripPrefix)); err != nil {
		return nil
	}

	// calculate etag
	chunk := make([]byte, 2048)
	n := 0
	hash := md5.New()
	for err == nil {
		n, err = f.Read(chunk)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}
		rb := chunk[:n]
		hash.Write(rb)
	}
	if err != nil && err != io.EOF {
		return err
	}

	// put inside kv
	var etagFormat = "\"%x\""
	etag := fmt.Sprintf(etagFormat, hash.Sum(nil))
	kv.CAS(urlPath, etag, func(v interface{}, pe error) bool {
		if pe != tinykv.ErrNotFound {
			return false
		}
		return true
	},
		tinykv.ExpiresAfter(time.Second*time.Duration(cco.maxAge)))

	return nil
}

//-----------------------------------------------------------------------------

// CacheControl is a middleware for adding ETag and Cache-Control headers,
// and checks If-None-Match client header
func CacheControl(fs http.FileSystem, options ...Option) func(http.Handler) http.Handler {
	var cco ccoptions
	for _, opt := range options {
		cco = opt(cco)
	}

	if cco.maxAge <= 0 {
		cco.maxAge = defaultMaxAge
	}

	cco.fs = fs

	return func(next http.Handler) http.Handler {
		h := func(rw http.ResponseWriter, rq *http.Request) {
			etag, ok := kv.Get(rq.URL.Path)
			if !ok {
				// 1
				go calculateETag(rq.URL.Path, fs, cco)
				// 2
				next.ServeHTTP(rw, rq)
				return
			}

			// add headers
			accessScope := "public"
			if cco.isPrivate {
				accessScope = "private"
			}
			if !devMode {
				rw.Header().Set(cacheControlServer,
					fmt.Sprintf(accessScope+", max-age=%d", cco.maxAge))
				rw.Header().Set(etagHeaderServer, etag.(string))
			} else {
				rw.Header().Set(cacheControlServer,
					fmt.Sprintf(accessScope+", max-age=%d", 1))
				rw.Header().Set(etagHeaderServer, fmt.Sprintf("%v", time.Now().UnixNano()))
			}

			// if client sent If-None-Match then just answer with http.StatusNotModified/304
			// and do not send actual content
			if found, ok := rq.Header[etagHeaderClient]; ok && found[0] == etag {
				rw.WriteHeader(http.StatusNotModified)
				return
			}

			next.ServeHTTP(rw, rq)
		}
		return http.HandlerFunc(h)
	}
}

//-----------------------------------------------------------------------------
