package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/time/rate"

	"news-api/db"
	"news-api/handlers"
)

var RssSources = []string{
	// Cybersecurity News
	"https://www.bleepingcomputer.com/feed/",
	"https://feeds.feedburner.com/TheHackersNews",
	"https://blogs.cisco.com/security/feed",
	"https://www.wired.com/feed/category/security/latest/rss",
	"https://www.securityweek.com/feed/",
	"https://news.sophos.com/en-us/feed/",
	"https://www.csoonline.com/feed/",
	// Tech News
	"https://www.theverge.com/rss/index.xml",
	"https://techcrunch.com/feed/",
	"https://arstechnica.com/feed/",
	"http://www.engadget.com/rss-full.xml",
	"http://www.fastcodesign.com/rss.xml",
	"http://www.forbes.com/entrepreneurs/index.xml",
	"https://blog.pragmaticengineer.com/rss/",
	"https://browser.engineering/rss.xml",
	"https://githubengineering.com/atom.xml",
	"https://joshwcomeau.com/rss.xml",
	"https://jvns.ca/atom.xml",
	"https://overreacted.io/rss.xml",
	"https://signal.org/blog/rss.xml",
	"https://slack.engineering/feed",
	"https://stripe.com/blog/feed.rss",
	// Defense News
	"https://www.defenseone.com/rss/all/",
	"https://thediplomat.com/category/asia-defense/feed/",
	"https://www.janes.com/osint-insights/defence-news/feed/",
	"https://www.militarytimes.com/arc/outboundfeeds/news-rss/",
	"https://www.defensenews.com/arc/outboundfeeds/home-rss/",
}

// Create a more generous rate limiter that allows 2 requests per second with a burst size of 10.
var limiter = rate.NewLimiter(2, 10)

func main() {
	if err := db.InitDB("./news.db"); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Check if we need to restore from CSV backup
	count, err := db.GetArticleCount()
	if err != nil {
		log.Printf("Warning: Failed to get article count: %v", err)
	} else if count == 0 {
		// Database is empty, try to load from CSV backup
		csvPath := "./articles.csv"
		if _, err := os.Stat(csvPath); err == nil {
			log.Println("Database is empty, loading articles from CSV backup...")
			if err := db.LoadArticlesFromCSV(csvPath); err != nil {
				log.Printf("Warning: Failed to load articles from CSV: %v", err)
			}
		} else {
			log.Println("No CSV backup file found, starting with empty database.")
		}
	}

	// Start the background caching job
	db.StartCachingJob(RssSources)

	// Start the self-ping mechanism to keep the service alive on free tiers.
	go startSelfPing()

	// The main handler is now wrapped in our security middlewares.
	mux := http.NewServeMux()
	fs := http.FileServer(http.Dir("./test"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	mux.HandleFunc("/news", handlers.GetNews)
	mux.HandleFunc("/today-threat", handlers.GetTodayThreat)
	mux.HandleFunc("/export/csv", handlers.ExportCSV)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Chain the middlewares. The request will flow from logging to security headers to the rate limiter.
	handler := loggingMiddleware(securityHeadersMiddleware(rateLimitMiddleware(mux)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server starting on port " + port + "...")
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

// Middleware for logging requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %s", r.Method, r.RequestURI, r.RemoteAddr, time.Since(start))
	})
}

// Middleware to add security headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline';")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// startSelfPing periodically pings the /healthz endpoint to keep the service alive on free hosting tiers.
func startSelfPing() {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		log.Println("APP_URL not set, self-pinging disabled.")
		return
	}

	healthzURL := appURL + "/healthz"
	ticker := time.NewTicker(4 * time.Minute) // Ping every 4 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("Pinging self at", healthzURL)
			resp, err := http.Get(healthzURL)
			if err != nil {
				log.Printf("Self-ping failed: %v", err)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				log.Printf("Self-ping returned non-200 status: %s", resp.Status)
			}
			resp.Body.Close()
		}
	}
}

// Middleware for rate limiting, which excludes the /healthz endpoint.
func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exclude the /healthz endpoint from rate limiting.
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
