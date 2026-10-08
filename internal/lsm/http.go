package lsm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type api struct{ e *Engine }

func (e *Engine) Handler() http.Handler {
	a := api{e: e}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, e.Stats()) })
	m.HandleFunc("POST /v1/flush", func(w http.ResponseWriter, r *http.Request) {
		if err := e.Flush(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("POST /v1/compact", func(w http.ResponseWriter, r *http.Request) {
		if err := e.Compact(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	m.HandleFunc("/v1/kv/", a.keyValue)
	return m
}
func (a api) keyValue(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.EscapedPath(), "/v1/kv/")
	key, err := url.PathUnescape(raw)
	if err != nil || key == "" {
		http.Error(w, "invalid key", 400)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Value string `json:"value"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
		d.DisallowUnknownFields()
		if err = d.Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", 400)
			return
		}
		if err = a.e.Put([]byte(key), []byte(body.Value)); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	case http.MethodGet:
		v, err := a.e.Get([]byte(key))
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, 200, map[string]string{"key": key, "value": string(v)})
	case http.MethodDelete:
		if err = a.e.Delete([]byte(key)); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", 405)
	}
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
