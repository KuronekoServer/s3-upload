package main

import (
	_ "embed"
	"net/http"
)

//go:embed docs.html
var docsHTML []byte

//go:embed openapi.json
var openAPISpec []byte

func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(docsHTML)
}

func openAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec)
}
