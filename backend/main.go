package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/just-pr/backend/ai"
	"github.com/just-pr/backend/handlers"
	ghclient "github.com/just-pr/backend/github"
)

func main() {
	// Load .env (ignore error if file doesn't exist — env vars may be set externally)
	_ = godotenv.Load()

	port := envOrDefault("PORT", "8080")
	corsOrigins := envOrDefault("CORS_ORIGINS", "*")

	// GitHub client
	ghToken := requireEnv("GITHUB_TOKEN")
	ghClient := ghclient.NewClient(ghToken)

	// AI provider
	provider := buildProvider()
	log.Printf("Using AI provider: %s", provider.Name())

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handlers.HealthHandler())
	mux.HandleFunc("/analyze", handlers.AnalyzeHandler(ghClient, provider))

	// Wrap with CORS middleware
	handler := corsMiddleware(corsOrigins, mux)

	addr := ":" + port
	log.Printf("JUST-PR backend listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func buildProvider() ai.Provider {
	providerName := envOrDefault("AI_PROVIDER", "claude")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	model := os.Getenv("ANTHROPIC_MODEL")

	switch strings.ToLower(providerName) {
	case "claude":
		// If a custom base URL is set, assume it's an OpenAI-compatible proxy (e.g. LiteLLM)
		// since those proxies speak /v1/chat/completions, not /v1/messages.
		if baseURL != "" {
			log.Printf("ANTHROPIC_BASE_URL is set — using OpenAI-compatible mode against %s", baseURL)
			apiKey := requireEnv("ANTHROPIC_API_KEY")
			return ai.NewOpenAICompatProvider(apiKey, baseURL, model)
		}
		apiKey := requireEnv("ANTHROPIC_API_KEY")
		return ai.NewClaudeProvider(apiKey, "", model)
	case "openai":
		apiKey := requireEnv("OPENAI_API_KEY")
		return ai.NewOpenAICompatProvider(apiKey, baseURL, model)
	default:
		log.Fatalf("Unknown AI provider: %q. Supported: claude, openai", providerName)
		panic("unreachable")
	}
}

func corsMiddleware(allowedOrigins string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowedOrigins == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Required environment variable %s is not set. Copy .env.example to .env and fill it in.", key)
	}
	return v
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
