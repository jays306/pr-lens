package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pr-lens/backend/app"
	"github.com/pr-lens/backend/cache"
	"github.com/pr-lens/backend/handler"
)

func main() {
	app.LoadDotEnv()

	port := envOrDefault("PORT", "8080")
	corsOrigins := envOrDefault("CORS_ORIGINS", "https://github.com")

	ghTokenDefault := app.GitHubToken()
	requireClientToken := strings.EqualFold(os.Getenv("REQUIRE_CLIENT_GITHUB_TOKEN"), "true")
	if ghTokenDefault == "" {
		log.Printf("GITHUB_TOKEN is not set — clients must send Authorization: Bearer <token> (e.g. from the Chrome extension)")
	}
	if requireClientToken {
		log.Printf("REQUIRE_CLIENT_GITHUB_TOKEN=true — /analyze will not use the server GITHUB_TOKEN")
	}

	analyzer, err := app.AnalyzerFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Using AI provider: %s", analyzer.Name())

	cacheTTL := 24 * time.Hour
	analysisCache := cache.New(cacheTTL)
	log.Printf("Analysis cache enabled (version=%s ttl=%s)", cache.Version, cacheTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.Health())
	mux.HandleFunc("/analyze", handler.Analyze(ghTokenDefault, analyzer, analysisCache, requireClientToken))
	mux.HandleFunc("/review", handler.Review())

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           corsMiddleware(corsOrigins, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       10 * time.Minute,
	}
	log.Printf("PR-LENS backend listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func corsMiddleware(allowedOrigins string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins == "*" {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
		} else {
			for _, allowed := range strings.Split(allowedOrigins, ",") {
				if strings.TrimSpace(allowed) == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-GitHub-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
