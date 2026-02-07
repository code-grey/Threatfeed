# Cybersecurity News Aggregator: Stay Ahead of the Threat

Tired of hopping between different news sites to get your daily dose of cybersecurity info? This project brings the latest headlines from top sources like The Hacker News and Bleeping Computer right to your screen. It's your one-stop-shop for everything from zero-day exploits to malware analysis.

[It's live!](https://newsaggregator-hisw.onrender.com/static/)

## Features

*   **Aggregated News Feed:** Get a consolidated view of the latest articles from multiple cybersecurity news sources.
*   **Keyword-Based Ranking:** Articles are automatically ranked based on important cybersecurity keywords, so you can see what's most relevant at a glance.
*   **Filtering and Sorting:** Customize your news feed by source, date, and rank.
*   **Simple Web Interface:** A clean and easy-to-use web interface to browse the news.
*   **Automatic Backup:** Nightly backup of articles to GitHub, with automatic restore on service restart.
*   **Open Source:** This project is open source, and we welcome contributions from the community.

## Getting Started

### For Developers

Want to run the project locally? Here's how to get started:

1.  **Clone the repository:**
    ```
    git clone https://github.com/code-grey/Threatfeed.git
    ```
2.  **Navigate to the `news-api` directory:**
    ```
    cd news-api
    ```
3.  **Run the backend server:**
    ```
    go run main.go
    ```
    The server will start on `http://localhost:8080`.

4.  **Open the web interface:**
    The web interface is located in `news-api/test/index.html`. You can open this file directly in your browser.

## Deployment

### Setting Up Automatic Backups

The project includes a GitHub Actions workflow that backs up articles nightly to prevent data loss on ephemeral hosting platforms like Render.

**Setup steps:**

1. **Add the `APP_URL` secret:**
   - Go to your GitHub repository
   - Navigate to **Settings** → **Secrets and variables** → **Actions**
   - Click **New repository secret**
   - Name: `APP_URL`
   - Value: Your deployed service URL (e.g., `https://your-app.onrender.com`)

2. **Trigger the first backup:**
   - Go to **Actions** → **Backup Articles CSV**
   - Click **Run workflow** → **Run workflow**
   - This creates the initial `news-api/articles.csv` backup

3. **Automatic restore:**
   - When the service restarts with an empty database, it automatically loads articles from the backup CSV

**How it works:**
- The workflow runs daily at 2:00 AM UTC
- It fetches articles from your running service via `/export/csv`
- The CSV is committed to the repository
- On service restart, if the database is empty, articles are restored from the CSV

## Contributing

We'd love your help to make this project even better! Whether you're a seasoned developer or just starting out, there are plenty of ways to contribute. Check out the "Future Enhancements" section for ideas, or feel free to propose your own (just open an issue).

P.S. Got a pun about cybersecurity? We'd love to hear it. Seriously, append it to your final commit in the pull request.

## Future Enhancements

We have some exciting plans for the future, including:

*   **AI-Powered Summarization:** Using AI to generate concise summaries of articles (web scraping and cloud functions).
*   **Semantic Similarity Search:** Finding articles that are semantically similar to the one you're reading.
*   **Android Application:** A native Android app for a seamless mobile experience.
*   **And much more!**
