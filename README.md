# cache-control
cache-control middleware adds _ETag_ header (md5 of the content) and _Cache-Control_ header

## sample usage

Using chi router, we register this middleware before a `http.FileServer`:

```go
fs := http.Dir(`./assets`)
assetServer := http.FileServer(fs)
rt.Route("/assets/*", func(rt chi.Router) {
	rt.Use(cachecontrol.CacheControl(
		fs,
		cachecontrol.StripPrefix("/assets")))
	rt.Get(
		"/*",
		http.StripPrefix("/assets", assetServer).ServeHTTP)
})
```

# TODO

* [dirwatch](https://github.com/dc0d/dirwatch) can be used for watching directories of assets (next).
* more tests
