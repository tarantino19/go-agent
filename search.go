package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-shiori/go-readability"
)

// SearchTool implements web search using DuckDuckGo HTML SERP + content fetching
var SearchDefinition = ToolDefinition{
	Name:        "web_search",
	Description: "Search the web for information using DuckDuckGo. Returns search results with extracted content that can be used to enhance prompts or provide context for coding decisions.",
	InputSchema: SearchInputSchema,
	Function:    WebSearch,
}

type SearchInput struct {
	Query        string `json:"query" jsonschema_description:"The search query to find relevant information"`
	MaxResults   int    `json:"max_results,omitempty" jsonschema_description:"Maximum number of results to return (default: 5, max: 10)"`
	FetchContent bool   `json:"fetch_content,omitempty" jsonschema_description:"Whether to fetch and extract full content from result URLs (default: true)"`
}

var SearchInputSchema = GenerateSchema[SearchInput]()

type SearchResult struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Snippet     string    `json:"snippet"`
	Content     string    `json:"content,omitempty"`
	Source      string    `json:"source"`
	RetrievedAt time.Time `json:"retrieved_at"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
	Summary string         `json:"summary"`
}

func WebSearch(input json.RawMessage) (string, error) {
	var searchInput SearchInput
	err := json.Unmarshal(input, &searchInput)
	if err != nil {
		return "", fmt.Errorf("failed to parse search input: %w", err)
	}

	if searchInput.Query == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	// Set defaults
	if searchInput.MaxResults == 0 {
		searchInput.MaxResults = 5
	}
	if searchInput.MaxResults > 10 {
		searchInput.MaxResults = 10
	}

	// Perform DuckDuckGo search
	results, err := searchDuckDuckGo(searchInput.Query, searchInput.MaxResults)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	// Fetch content if requested
	if searchInput.FetchContent {
		results = fetchResultsContent(results)
	}

	// Create response
	response := SearchResponse{
		Query:   searchInput.Query,
		Results: results,
		Summary: generateSearchSummary(searchInput.Query, results),
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}

func searchDuckDuckGo(query string, maxResults int) ([]SearchResult, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Build DuckDuckGo HTML search URL
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	// Create request with proper headers
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("search request failed with status: %d", resp.StatusCode)
	}

	// Parse HTML response
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var results []SearchResult
	retrievedAt := time.Now()

	// Extract search results from DuckDuckGo HTML
	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if len(results) >= maxResults {
			return
		}

		// Extract title and URL
		titleLink := s.Find(".result__title a")
		title := strings.TrimSpace(titleLink.Text())
		href, exists := titleLink.Attr("href")
		if !exists || title == "" {
			return
		}

		// Clean up the URL (DuckDuckGo wraps URLs)
		actualURL := cleanDuckDuckGoURL(href)
		if actualURL == "" {
			return
		}

		// Extract snippet
		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())

		// Create result ID
		resultID := fmt.Sprintf("search_%d_%x", i, time.Now().UnixNano())

		result := SearchResult{
			ID:          resultID,
			Title:       title,
			URL:         actualURL,
			Snippet:     snippet,
			Source:      "ddg-html",
			RetrievedAt: retrievedAt,
		}

		results = append(results, result)
	})

	if len(results) == 0 {
		return nil, fmt.Errorf("no search results found")
	}

	return results, nil
}

func cleanDuckDuckGoURL(duckURL string) string {
	// DuckDuckGo wraps URLs like: /l/?uddg=https%3A//example.com
	if strings.HasPrefix(duckURL, "/l/?uddg=") {
		encoded := strings.TrimPrefix(duckURL, "/l/?uddg=")
		decoded, err := url.QueryUnescape(encoded)
		if err == nil {
			return decoded
		}
	}

	// Try direct URL if it looks like a proper URL
	if strings.HasPrefix(duckURL, "http://") || strings.HasPrefix(duckURL, "https://") {
		return duckURL
	}

	return ""
}

func fetchResultsContent(results []SearchResult) []SearchResult {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	for i := range results {
		content, err := fetchAndExtractContent(client, results[i].URL)
		if err != nil {
			// Log error but continue with other results
			fmt.Printf("Failed to fetch content from %s: %v\n", results[i].URL, err)
			continue
		}

		results[i].Content = content
		results[i].Source = "ddg-html+fetch"
	}

	return results
}

func fetchAndExtractContent(client *http.Client, targetURL string) (string, error) {
	// Create request
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Extract main content using readability
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return extractTextFallback(string(body))
	}

	article, err := readability.FromReader(strings.NewReader(string(body)), parsedURL)
	if err != nil {
		// Fallback: extract text from HTML using goquery
		return extractTextFallback(string(body))
	}

	// Clean and truncate content
	content := cleanExtractedText(article.TextContent)
	if len(content) > 2000 {
		content = content[:2000] + "..."
	}

	return content, nil
}

func extractTextFallback(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// Remove script and style elements
	doc.Find("script, style, nav, header, footer, aside").Remove()

	// Extract text from main content areas
	var text strings.Builder
	doc.Find("main, article, .content, .post, .entry, p, h1, h2, h3, h4, h5, h6").Each(func(i int, s *goquery.Selection) {
		textContent := strings.TrimSpace(s.Text())
		if textContent != "" {
			text.WriteString(textContent)
			text.WriteString("\n")
		}
	})

	content := cleanExtractedText(text.String())
	if len(content) > 2000 {
		content = content[:2000] + "..."
	}

	return content, nil
}

func cleanExtractedText(text string) string {
	// Remove extra whitespace and normalize
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	// Remove common boilerplate
	text = regexp.MustCompile(`(?i)(cookie|privacy policy|terms of service|subscribe|newsletter)`).ReplaceAllString(text, "")

	return text
}

func generateSearchSummary(query string, results []SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for query: %s", query)
	}

	summary := fmt.Sprintf("Found %d results for '%s':\n", len(results), query)

	for i, result := range results {
		contentNote := ""
		if result.Content != "" {
			contentNote = " (with extracted content)"
		}
		summary += fmt.Sprintf("%d. %s - %s%s\n", i+1, result.Title, result.URL, contentNote)
	}

	summary += "\nUse this information to enhance your understanding and provide more accurate, up-to-date responses."

	return summary
}
