package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"news-api/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB is a helper to initialize a clean in-memory database for testing.
func setupTestDB(t *testing.T) {
	if err := InitDB(":memory:"); err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	// Clear articles table before running tests to ensure isolation
	_, err := db.Exec("DELETE FROM articles")
	if err != nil {
		t.Fatalf("Failed to clear articles table: %v", err)
	}
}

// teardownTestDB is a no-op since the in-memory database is ephemeral.
func teardownTestDB() {}

func TestCalculateRank(t *testing.T) {
	testCases := []struct {
		name     string
		article  models.NewsArticle
		expected int
	}{
		{
			name: "Cybersecurity High Impact",
			article: models.NewsArticle{
				Title:       "Critical zero-day exploit found",
				Description: "Active attack with ransomware attack confirmed.",
				Category:    "Cybersecurity",
			},
			expected: 24, // zero-day(5) + exploit(3) + active attack(5) + attack(3) + ransomware attack(5) + ransomware(3)
		},
		{
			name: "Cybersecurity Medium Impact",
			article: models.NewsArticle{
				Title:       "New malware advisory released",
				Description: "A threat actor is using phishing to spread it.",
				Category:    "Cybersecurity",
			},
			expected: 12, // malware(3) + advisory(3) + threat(3) + phishing(3)
		},
		{
			name: "Tech High Impact",
			article: models.NewsArticle{
				Title:       "AI breakthrough in quantum computing",
				Description: "This innovation will shape the future of tech.",
				Category:    "Tech",
			},
			expected: 25, // ai(5) + breakthrough(5) + quantum computing(5) + innovation(5) + future of tech(5)
		},
		{
			name: "Tech Low Impact",
			article: models.NewsArticle{
				Title:       "Review of the new gadget",
				Description: "Here are some tips for this software update.",
				Category:    "Tech",
			},
			expected: 5, // review(1) + gadget(1) + tips(1) + software(1) + update(1)
		},
		{
			name: "General Category",
			article: models.NewsArticle{
				Title:       "News update report",
				Description: "A general report.",
				Category:    "General",
			},
			expected: 3, // news(1) + update(1) + report(1)
		},
		{
			name: "No Keywords",
			article: models.NewsArticle{
				Title:       "An article",
				Description: "Some text.",
				Category:    "Cybersecurity",
			},
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rank := calculateRank(tc.article)
			assert.Equal(t, tc.expected, rank, "Rank calculation was incorrect")
		})
	}
}

func TestGetCategoryForSource(t *testing.T) {
	testCases := []struct {
		name      string
		sourceURL string
		expected  string
	}{
		{"Bleeping Computer", "https://www.bleepingcomputer.com/feed/", "Cybersecurity"},
		{"The Verge", "https://www.theverge.com/rss/index.xml", "Tech"},
		{"Defense One", "https://www.defenseone.com/rss/all/", "Defense"},
		{"Unknown Source", "http://example.com/feed", "General"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			category := getCategoryForSource(tc.sourceURL)
			assert.Equal(t, tc.expected, category, "Category was incorrect")
		})
	}
}

func TestGetTodayThreatScore(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	now := time.Now()
	// Insert articles with different ranks and timestamps
	articles := []models.NewsArticle{
		// High rank, recent
		{Title: "t1", URL: "u1", PublishedAt: now.Add(-1 * time.Hour), Rank: 10, Category: "Cybersecurity"},
		// High rank, recent
		{Title: "t2", URL: "u2", PublishedAt: now.Add(-2 * time.Hour), Rank: 5, Category: "Cybersecurity"},
		// Medium rank, recent
		{Title: "t3", URL: "u3", PublishedAt: now.Add(-3 * time.Hour), Rank: 4, Category: "Cybersecurity"},
		// Medium rank, recent
		{Title: "t4", URL: "u4", PublishedAt: now.Add(-4 * time.Hour), Rank: 2, Category: "Cybersecurity"},
		// Low rank, recent
		{Title: "t5", URL: "u5", PublishedAt: now.Add(-5 * time.Hour), Rank: 1, Category: "Cybersecurity"},
		// Low rank, recent
		{Title: "t6", URL: "u6", PublishedAt: now.Add(-6 * time.Hour), Rank: 0, Category: "Cybersecurity"},
		// Old article, should be ignored
		{Title: "t7", URL: "u7", PublishedAt: now.Add(-48 * time.Hour), Rank: 10, Category: "Cybersecurity"},
	}

	for _, article := range articles {
		err := InsertArticle(article)
		assert.NoError(t, err)
	}

	score, err := GetTodayThreatScore()
	assert.NoError(t, err)

	assert.Equal(t, 2, score.HighRankCount, "High rank count mismatch")
	assert.Equal(t, 2, score.MediumRankCount, "Medium rank count mismatch")
	assert.Equal(t, 2, score.LowRankCount, "Low rank count mismatch")
	assert.Equal(t, 6, score.TotalArticles, "Total articles count mismatch")
	assert.Equal(t, "Code Red", score.ThreatLevel, "Threat level mismatch")
}

func TestGetTodayThreatScoreLevels(t *testing.T) {
	testCases := []struct {
		name          string
		articles      []models.NewsArticle
		expectedLevel string
	}{
		{
			name:          "Code Red",
			articles:      []models.NewsArticle{{Title: "t1", URL: "u1", Rank: 5, PublishedAt: time.Now()}},
			expectedLevel: "Code Red",
		},
		{
			name:          "Attention",
			articles:      []models.NewsArticle{{Title: "t1", URL: "u1", Rank: 3, PublishedAt: time.Now()}},
			expectedLevel: "Attention",
		},
		{
			name:          "Business as Usual",
			articles:      []models.NewsArticle{{Title: "t1", URL: "u1", Rank: 1, PublishedAt: time.Now()}},
			expectedLevel: "Business as Usual",
		},
		{
			name:          "No Threats Reported",
			articles:      []models.NewsArticle{},
			expectedLevel: "No Threats Reported",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setupTestDB(t)
			defer teardownTestDB()

			for _, article := range tc.articles {
				err := InsertArticle(article)
				assert.NoError(t, err)
			}

			score, err := GetTodayThreatScore()
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedLevel, score.ThreatLevel)
		})
	}
}

func TestGetArticleCount(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	// Initially, the count should be 0
	count, err := GetArticleCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Insert some articles
	articles := []models.NewsArticle{
		{Title: "t1", URL: "u1", PublishedAt: time.Now(), Rank: 5, Category: "Cybersecurity"},
		{Title: "t2", URL: "u2", PublishedAt: time.Now(), Rank: 3, Category: "Tech"},
		{Title: "t3", URL: "u3", PublishedAt: time.Now(), Rank: 1, Category: "General"},
	}

	for _, article := range articles {
		err := InsertArticle(article)
		require.NoError(t, err)
	}

	// Now the count should be 3
	count, err = GetArticleCount()
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestLoadArticlesFromCSV(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	// Create a temporary CSV file
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test_articles.csv")

	csvContent := `Title,Description,ImageURL,URL,SourceURL,PublishedAt,Rank,Category
Test Article 1,Description for article 1,https://img.example.com/1.jpg,https://example.com/1,https://source.example.com,2024-01-15T10:30:00Z,5,Cybersecurity
Test Article 2,Description for article 2,https://img.example.com/2.jpg,https://example.com/2,https://source.example.com,2024-01-16T11:30:00Z,3,Tech
Test Article 3,Description for article 3,,https://example.com/3,https://source.example.com,2024-01-17T12:30:00Z,1,General
`
	err := os.WriteFile(csvPath, []byte(csvContent), 0644)
	require.NoError(t, err)

	// Initially, the count should be 0
	count, err := GetArticleCount()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Load articles from CSV
	err = LoadArticlesFromCSV(csvPath)
	require.NoError(t, err)

	// Now the count should be 3
	count, err = GetArticleCount()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify articles are stored correctly
	articles, err := GetArticlesFromDB("", "", "", 10, time.Time{}, time.Time{}, "")
	require.NoError(t, err)
	assert.Len(t, articles, 3)

	// Verify the first article
	found := false
	for _, a := range articles {
		if a.Title == "Test Article 1" {
			assert.Equal(t, "Description for article 1", a.Description)
			assert.Equal(t, "https://img.example.com/1.jpg", a.ImageURL)
			assert.Equal(t, "https://example.com/1", a.URL)
			assert.Equal(t, 5, a.Rank)
			assert.Equal(t, "Cybersecurity", a.Category)
			found = true
			break
		}
	}
	assert.True(t, found, "Test Article 1 should be found in the database")
}

func TestLoadArticlesFromCSV_FileNotFound(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	err := LoadArticlesFromCSV("/nonexistent/path/to/file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open CSV file")
}

func TestLoadArticlesFromCSV_InvalidFormat(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	// Create a temporary CSV file with invalid format
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "invalid_articles.csv")

	// Only 3 columns instead of 8
	csvContent := `Col1,Col2,Col3
val1,val2,val3
`
	err := os.WriteFile(csvPath, []byte(csvContent), 0644)
	require.NoError(t, err)

	err = LoadArticlesFromCSV(csvPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CSV header")
}

func TestLoadArticlesFromCSV_DuplicateArticles(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB()

	// Create a temporary CSV file with duplicate URLs
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "duplicate_articles.csv")

	csvContent := `Title,Description,ImageURL,URL,SourceURL,PublishedAt,Rank,Category
Test Article 1,Description 1,,https://example.com/1,https://source.example.com,2024-01-15T10:30:00Z,5,Cybersecurity
Test Article 1 Duplicate,Description 2,,https://example.com/1,https://source.example.com,2024-01-16T11:30:00Z,3,Tech
`
	err := os.WriteFile(csvPath, []byte(csvContent), 0644)
	require.NoError(t, err)

	// Load articles from CSV
	err = LoadArticlesFromCSV(csvPath)
	require.NoError(t, err)

	// Only 1 article should be inserted due to unique URL constraint
	count, err := GetArticleCount()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
