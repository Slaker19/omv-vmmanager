package api

import (
	"embed"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed novnc.mjs
var staticFS embed.FS

func staticRouter() http.Handler {
	r := chi.NewRouter()

	r.Get("/novnc.mjs", func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("novnc.mjs")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(data)
	})

	return r
}
