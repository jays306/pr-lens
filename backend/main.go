package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/pr-lens/backend/analysis"
	"github.com/pr-lens/backend/cache"
	"github.com/pr-lens/backend/handler"
	"github.com/pr-lens/backend/providers"
)

func main() {
	_ = godotenv.Load()

	port := envOrDefault("PORT", "8080")
	corsOrigins := envOrDefault("CORS_ORIGINS", "*")

	ghTokenDefault := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if ghTokenDefault == "" {
		log.Printf("GITHUB_TOKEN is not set — clients must send Authorization: Bearer <token> (e.g. from the Chrome extension)")
	}

	analyzer := buildAnalyzer()
	log.Printf("Using AI provider: %s", analyzer.Name())

	cacheTTL := 24 * time.Hour
	analysisCache := cache.New(cacheTTL)
	log.Printf("Analysis cache enabled (version=%s ttl=%s)", cache.Version, cacheTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.Health())
	mux.HandleFunc("/analyze", handler.Analyze(ghTokenDefault, analyzer, analysisCache))
	mux.HandleFunc("/review", handler.Review(ghTokenDefault))

	log.Printf("PR-LENS backend listening on :%s", port)
	if err := http.ListenAndServe(":"+port, corsMiddleware(corsOrigins, mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func buildAnalyzer() analysis.Analyzer {
	providerName := envOrDefault("AI_PROVIDER", "claude")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	model := os.Getenv("ANTHROPIC_MODEL")

	switch strings.ToLower(providerName) {
	case "claude":
		apiKey := requireEnv("ANTHROPIC_API_KEY")
		if baseURL != "" {
			log.Printf("ANTHROPIC_BASE_URL is set — routing through proxy at %s", baseURL)
		}
		p := providers.NewClaudeProvider(apiKey, baseURL, model)
		return analysis.NewPipelineProvider(analysis.PipelineConfig{
			APIKey:           apiKey,
			BaseURL:          baseURL,
			Caller:           p,
			FallbackAnalyzer: p,
		})
	case "bedrock":
		region := envOrDefault("BEDROCK_REGION", "us-east-1")
		apiKey := os.Getenv("BEDROCK_API_KEY") // empty → use standard AWS credential chain
		p := providers.NewBedrockProvider(apiKey, region, model)
		return analysis.NewPipelineProvider(analysis.PipelineConfig{
			Caller:           p,
			FallbackAnalyzer: p,
		})
	case "openai":
		p := providers.NewOpenAIProvider(requireEnv("OPENAI_API_KEY"), baseURL, model)
		return analysis.NewPipelineProvider(analysis.PipelineConfig{
			APIKey:           os.Getenv("ANTHROPIC_API_KEY"), // optional: triage uses Haiku if available
			Caller:           p,
			FallbackAnalyzer: p,
		})
	default:
		log.Fatalf("Unknown AI provider: %q. Supported: claude, bedrock, openai", providerName)
		panic("unreachable")
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
