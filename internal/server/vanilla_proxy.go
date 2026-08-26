// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// VanillaReferenceMux serves frozen web/ and reverse-proxies the rest
// to the daily React/API process (🎯T540.4). UI-only: no state, no fleet.
func VanillaReferenceMux(webDir string, upstream *url.URL) http.Handler {
	mux := http.NewServeMux()
	RegisterUIRoutes(mux, webDir)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.FlushInterval = -1
	mux.Handle("/", proxy)
	return mux
}
