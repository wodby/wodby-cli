package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWritesSocialMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := generate(dir); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	tests := []struct {
		file        string
		title       string
		description string
		canonical   string
	}{
		{
			file:        "index.html",
			title:       "Wodby CLI manual",
			description: indexDescription,
			canonical:   "https://wodby.com/docs/2.0/cli/",
		},
		{
			file:        "wodby_project_list.html",
			title:       "wodby project list | Wodby CLI",
			description: "wodby project list command reference for Wodby CLI. List projects.",
			canonical:   "https://wodby.com/docs/2.0/cli/wodby_project_list.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}

			html := string(content)
			assertContains(t, html, `<title>`+tt.title+`</title>`)
			assertContains(t, html, `<meta name="description" content="`+tt.description+`">`)
			assertContains(t, html, `<link rel="canonical" href="`+tt.canonical+`">`)
			assertContains(t, html, `<meta property="og:type" content="website">`)
			assertContains(t, html, `<meta property="og:site_name" content="Wodby Documentation">`)
			assertContains(t, html, `<meta property="og:title" content="`+tt.title+`">`)
			assertContains(t, html, `<meta property="og:description" content="`+tt.description+`">`)
			assertContains(t, html, `<meta property="og:url" content="`+tt.canonical+`">`)
			assertContains(t, html, `<meta name="twitter:card" content="summary">`)
			assertContains(t, html, `<meta name="twitter:title" content="`+tt.title+`">`)
			assertContains(t, html, `<meta name="twitter:description" content="`+tt.description+`">`)
			assertContains(t, html, `<link rel="sitemap" type="application/xml" href="https://wodby.com/docs/2.0/cli/sitemap.xml">`)
		})
	}
}

func TestGenerateWritesSitemap(t *testing.T) {
	dir := t.TempDir()
	if err := generate(dir); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "sitemap.xml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	sitemapXML := string(content)
	assertContains(t, sitemapXML, `<?xml version="1.0" encoding="UTF-8"?>`)
	assertContains(t, sitemapXML, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	assertContains(t, sitemapXML, `<loc>https://wodby.com/docs/2.0/cli/</loc>`)
	assertContains(t, sitemapXML, `<loc>https://wodby.com/docs/2.0/cli/wodby.html</loc>`)
	assertContains(t, sitemapXML, `<loc>https://wodby.com/docs/2.0/cli/wodby_project_list.html</loc>`)

	if strings.Contains(sitemapXML, "https://wodby.com/docs/2.0/cli/index.html") {
		t.Fatalf("sitemap should use the directory canonical for the index page")
	}

	htmlFiles, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if got, want := strings.Count(sitemapXML, "<url>"), len(htmlFiles); got != want {
		t.Fatalf("sitemap URL count = %d, want %d", got, want)
	}
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("generated HTML missing %q", want)
	}
}
