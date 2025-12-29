package db

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"news-api/models"

	_ "github.com/mattn/go-sqlite3"
	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"github.com/pemistahl/lingua-go"
)

var db *sql.DB
var detector lingua.LanguageDetector

// dbMutex protects database write operations to prevent race conditions
// during CSV import and RSS caching jobs.
var dbMutex sync.Mutex

func InitDB(dataSourceName string) error {
	var err error
	db, err = sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		description TEXT,
		imageUrl TEXT,
		url TEXT NOT NULL UNIQUE,
		sourceUrl TEXT NOT NULL,
		publishedAt DATETIME DEFAULT CURRENT_TIMESTAMP,
		rank INTEGER DEFAULT 0,
		category TEXT DEFAULT ''
	);
	`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create articles table: %v", err)
	}

	// Create indexes for faster queries
	createIndexesSQL := `
	CREATE INDEX IF NOT EXISTS idx_sourceUrl ON articles (sourceUrl);
	CREATE INDEX IF NOT EXISTS idx_publishedAt ON articles (publishedAt);
	`
	_, err = db.Exec(createIndexesSQL)
	if err != nil {
		return fmt.Errorf("failed to create indexes: %v", err)
	}

	// Optimize language detector to only load models for relevant languages
	detector = lingua.NewLanguageDetectorBuilder().
		FromLanguages(lingua.English, lingua.German, lingua.French, lingua.Spanish, lingua.Russian, lingua.Chinese).
		WithPreloadedLanguageModels().
		Build()

	log.Println("Database initialized successfully.")
	return nil
}

func calculateRank(article models.NewsArticle) int {
	rank := 0
	content := strings.ToLower(article.Title + " " + article.Description)

	var keywords map[string]int

	switch article.Category {
	case "Cybersecurity":
		keywords = map[string]int{
			// High Impact (Score 5): Direct, immediate threats
			"zero-day": 5, "exploit in the wild": 5, "active attack": 5, "critical vulnerability": 5, "alert": 5, "warning": 5, "patch now": 5, "ransomware attack": 5, "breach confirmed": 5,
			// Medium Impact (Score 3): Significant threats, but perhaps not immediate action required
			"vulnerability": 3, "exploit": 3, "breach": 3, "attack": 3, "malware": 3, "ransomware": 3, "phishing": 3, "threat": 3, "advisory": 3,
			// Low Impact (Score 1): General cybersecurity news, informative
			"security": 1, "cybersecurity": 1, "data": 1, "privacy": 1, "risk": 1, "compliance": 1, "encryption": 1, "patch": 1,
		}
	case "Tech":
		keywords = map[string]int{
			// High Impact (Score 5): Major announcements, breakthroughs, critical issues
			"ai": 5, "artificial intelligence": 5, "quantum computing": 5, "breakthrough": 5, "major update": 5, "new chip": 5, "innovation": 5, "future of tech": 5,
			// Medium Impact (Score 3): Significant developments, new products, industry trends
			"startup": 3, "funding": 3, "acquisition": 3, "cloud": 3, "5g": 3, "machine learning": 3, "data science": 3, "web3": 3, "metaverse": 3, "robotics": 3,
			// Low Impact (Score 1): General tech news, reviews, minor updates
			"review": 1, "gadget": 1, "app": 1, "software": 1, "hardware": 1, "update": 1, "guide": 1, "tips": 1,
		}
	default: // General or unknown category
		keywords = map[string]int{
			"news": 1, "update": 1, "report": 1,
		}
	}

	for keyword, score := range keywords {
		if strings.Contains(content, keyword) {
			rank += score
		}
	}

	return rank
}

func InsertArticle(article models.NewsArticle) error {
	stmt, err := db.Prepare("INSERT OR IGNORE INTO articles(title, description, imageUrl, url, sourceUrl, publishedAt, rank, category) VALUES(?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Printf("Error preparing insert statement for article %s: %v", article.Title, err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(article.Title, article.Description, article.ImageURL, article.URL, article.SourceURL, article.PublishedAt, article.Rank, article.Category)
	if err != nil {
		log.Printf("Error inserting article %s: %v", article.Title, err)
	}
	return err
}

// ThreatScore represents the calculated threat score and its corresponding phrase.
type ThreatScore struct {
	LowRankCount    int    `json:"lowRankCount"`
	MediumRankCount int    `json:"mediumRankCount"`
	HighRankCount   int    `json:"highRankCount"`
	TotalArticles   int    `json:"totalArticles"`
	ThreatLevel     string `json:"threatLevel"`
}

// GetTodayThreatScore calculates the threat score based on articles published in the last 24 hours.
func GetTodayThreatScore() (ThreatScore, error) {
	var lowRankCount, mediumRankCount, highRankCount int
	var totalArticles int

	// Calculate the time 24 hours ago from the current time.
	twentyFourHoursAgo := time.Now().Add(-24 * time.Hour)

	rows, err := db.Query("SELECT rank FROM articles WHERE publishedAt >= ?", twentyFourHoursAgo.Format("2006-01-02 15:04:05"))
	if err != nil {
		return ThreatScore{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var rank int
		if err := rows.Scan(&rank); err != nil {
			log.Printf("Error scanning rank for threat score: %v", err)
			continue
		}
		totalArticles++
		// Define rank ranges for low, medium, high
		if rank < 2 { // Ranks 0-1 are considered low
			lowRankCount++
		} else if rank < 5 { // Ranks 2-4 are medium
			mediumRankCount++
		} else { // Ranks 5+ are high
			highRankCount++
		}
	}

	var threatLevel string
	if totalArticles == 0 {
		threatLevel = "No Threats Reported"
	} else if highRankCount > 0 {
		threatLevel = "Code Red"
	} else if mediumRankCount > 0 {
		threatLevel = "Attention"
	} else {
		threatLevel = "Business as Usual"
	}

	return ThreatScore{
		LowRankCount:    lowRankCount,
		MediumRankCount: mediumRankCount,
		HighRankCount:   highRankCount,
		TotalArticles:   totalArticles,
		ThreatLevel:     threatLevel,
	}, nil
}

func GetArticlesFromDB(sourceFilter string, categoryFilter string, searchFilter string, limit int, startDate, endDate time.Time, sortBy string) ([]models.NewsArticle, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	var articles []models.NewsArticle
	query := "SELECT title, description, imageUrl, url, sourceUrl, publishedAt, rank, category FROM articles"
	args := []interface{}{}

	whereClauses := []string{}

	if sourceFilter != "" && sourceFilter != "all" {
		whereClauses = append(whereClauses, "sourceUrl = ?")
		args = append(args, sourceFilter)
	}

	if categoryFilter != "" && categoryFilter != "all" {
		whereClauses = append(whereClauses, "category = ?")
		args = append(args, categoryFilter)
	}

	if searchFilter != "" {
		whereClauses = append(whereClauses, "(LOWER(title) LIKE ? OR LOWER(description) LIKE ?)")
		searchPattern := "%" + strings.ToLower(searchFilter) + "%"
		args = append(args, searchPattern, searchPattern)
	}

	if !startDate.IsZero() {
		whereClauses = append(whereClauses, "publishedAt >= ?")
		args = append(args, startDate.Format("2006-01-02 15:04:05"))
	}
	if !endDate.IsZero() {
		whereClauses = append(whereClauses, "publishedAt <= ?")
		args = append(args, endDate.Format("2006-01-02 15:04:05"))
	}

	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	if sortBy == "rank" {
		query += " ORDER BY rank DESC"
	} else {
		query += " ORDER BY publishedAt DESC"
	}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Error executing query in GetArticlesFromDB: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var article models.NewsArticle
		if err := rows.Scan(&article.Title, &article.Description, &article.ImageURL, &article.URL, &article.SourceURL, &article.PublishedAt, &article.Rank, &article.Category); err != nil {
			log.Printf("Error scanning article: %v", err)
			continue
		}
		articles = append(articles, article)
	}

	return articles, nil
}

func StartCachingJob(rssSources []string) {
	fetchAndCacheNews(rssSources)

	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		for range ticker.C {
			log.Println("Running scheduled news caching job...")
			fetchAndCacheNews(rssSources)
		}
	}()
}

func fetchAndCacheNews(rssSources []string) {
	client := &http.Client{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client.Transport = &userAgentTransport{RoundTripper: transport}

	fp := gofeed.NewParser()
	fp.Client = client

	var wg sync.WaitGroup
	p := bluemonday.StripTagsPolicy()

	articleChan := make(chan models.NewsArticle, 100)

	go func() {
		for article := range articleChan {
			InsertArticle(article) // This runs strictly one at a time
		}
	}()

	for _, source := range rssSources {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			feed, err := fp.ParseURL(source)
			if err != nil {
				log.Printf("Error parsing feed from %s for caching: %v", source, err)
				return
			}

			for _, item := range feed.Items {
				// Language detection
				textToDetect := item.Title + " " + item.Description
				lang, _ := detector.DetectLanguageOf(textToDetect)
				if lang != lingua.English {
					log.Printf("Skipping non-English article: %s (Source: %s)", item.Title, source)
					continue
				}

				category := getCategoryForSource(source)

				article := models.NewsArticle{
					Title:       item.Title,
					Description: p.Sanitize(item.Description),
					URL:         item.Link,
					SourceURL:   source,
					Category:    category,
				}
				article.Rank = calculateRank(article)

				if item.Image != nil {
					article.ImageURL = item.Image.URL
				}
				if item.PublishedParsed != nil {
					article.PublishedAt = *item.PublishedParsed
				} else if feed.PublishedParsed != nil {
					article.PublishedAt = *feed.PublishedParsed
				} else {
					article.PublishedAt = time.Now()
				}

				// Send to the channel instead of writing to DB
				articleChan <- article
			}
		}(source)
	}

	wg.Wait()
	close(articleChan)
	log.Println("News caching job completed.")
}

type userAgentTransport struct {
	http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")
	return t.RoundTripper.RoundTrip(req)
}

func getCategoryForSource(sourceURL string) string {
	// Define your source-to-category mapping here
	cybersecuritySources := []string{
		"https://www.bleepingcomputer.com/feed/",
		"https://feeds.feedburner.com/TheHackersNews",
		"https://blogs.cisco.com/security/feed",
		"https://www.wired.com/feed/category/security/latest/rss",
		"https://www.securityweek.com/feed/",
		"https://news.sophos.com/en-us/feed/",
		"https://www.csoonline.com/feed/",
	}

	techSources := []string{
		"https://www.theverge.com/rss/index.xml",
		"https://techcrunch.com/feed/",
		"https://arstechnica.com/feed/",
		"http://www.engadget.com/rss-full.xml",
		"http://www.fastcodesign.com/rss.com",
		"http://www.forbes.com/entrepreneurs/index.xml",
		"https://blog.pragmaticengineer.com/rss/",
		"https://browser.engineering/rss.xml",
		"https://githubengineering.com/atom.com",
		"https://joshwcomeau.com/rss.xml",
		"https://jvns.ca/atom.xml",
		"https://overreacted.io/rss.com",
		"https://signal.org/blog/rss.com",
		"https://slack.engineering/feed",
		"https://stripe.com/blog/feed.rss",
	}

	defenseSources := []string{
		"https://www.defenseone.com/rss/all/",
		"https://thediplomat.com/category/asia-defense/feed/",
		"https://www.janes.com/osint-insights/defence-news/feed/",
		"https://www.militarytimes.com/arc/outboundfeeds/news-rss/",
		"https://www.defensenews.com/arc/outboundfeeds/home-rss/",
	}

	for _, s := range cybersecuritySources {
		if s == sourceURL {
			return "Cybersecurity"
		}
	}

	for _, s := range techSources {
		if s == sourceURL {
			return "Tech"
		}
	}

	for _, s := range defenseSources {
		if s == sourceURL {
			return "Defense"
		}
	}

	return "General" // Default category if no match
}

// ClearAllArticlesForTest clears all articles from the database. This is intended for use in tests.
func ClearAllArticlesForTest() error {
	if db == nil {
		return nil
	}
	_, err := db.Exec("DELETE FROM articles")
	return err
}

// GetAllArticlesStream returns a sql.Rows object for streaming all articles.
// The caller is responsible for closing the rows.
func GetAllArticlesStream() (*sql.Rows, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	query := "SELECT title, description, imageUrl, url, sourceUrl, publishedAt, rank, category FROM articles ORDER BY publishedAt DESC"
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetArticleCount returns the number of articles in the database.
func GetArticleCount() (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM articles").Scan(&count)
	return count, err
}

// LoadArticlesFromCSV loads articles from a CSV file into the database.
// This function is used to restore articles after a service restart.
// It uses a mutex to prevent race conditions with the caching job.
func LoadArticlesFromCSV(filePath string) error {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read and skip the header row
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %v", err)
	}

	// Validate header format
	expectedHeaders := []string{"Title", "Description", "ImageURL", "URL", "SourceURL", "PublishedAt", "Rank", "Category"}
	if len(header) != len(expectedHeaders) {
		return fmt.Errorf("invalid CSV header: expected %d columns, got %d", len(expectedHeaders), len(header))
	}

	// Prepare the insert statement
	stmt, err := db.Prepare("INSERT OR IGNORE INTO articles(title, description, imageUrl, url, sourceUrl, publishedAt, rank, category) VALUES(?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %v", err)
	}
	defer stmt.Close()

	importedCount := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading CSV record: %v", err)
			continue
		}

		if len(record) != 8 {
			log.Printf("Skipping invalid record with %d columns", len(record))
			continue
		}

		// Parse published date - skip record if date is invalid
		publishedAt, err := time.Parse(time.RFC3339, record[5])
		if err != nil {
			log.Printf("Skipping article %s: invalid date format: %v", record[0], err)
			continue
		}

		// Parse rank - skip record if rank is invalid
		rank, err := strconv.Atoi(record[6])
		if err != nil {
			log.Printf("Skipping article %s: invalid rank format: %v", record[0], err)
			continue
		}

		_, err = stmt.Exec(record[0], record[1], record[2], record[3], record[4], publishedAt, rank, record[7])
		if err != nil {
			log.Printf("Error inserting article from CSV: %v", err)
			continue
		}
		importedCount++
	}

	log.Printf("Loaded %d articles from CSV file: %s", importedCount, filePath)
	return nil
}
