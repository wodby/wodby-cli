package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/wodby/wodby-cli/cmd/wodby/root"
)

const (
	cliReferenceBaseURL = "https://wodby.com/docs/2.0/cli/"
	faviconPath         = "../assets/images/favicon.svg"
	siteName            = "Wodby Documentation"
	indexDescription    = "Command-line reference for Wodby 2.0, including commands, options, aliases, and examples."
)

type flagInfo struct {
	Name       string
	Usage      string
	Default    string
	HasDefault bool
	Deprecated string
}

type commandLink struct {
	Path  string
	Name  string
	File  string
	Short string
}

type navItem struct {
	Link     commandLink
	Children []navItem
}

type commandPage struct {
	Title        string
	Description  string
	CanonicalURL string
	Active       string
	Nav          []navItem
	Command      *commandInfo
}

type commandInfo struct {
	Path             string
	Name             string
	File             string
	Short            string
	Long             string
	Usage            string
	Aliases          []string
	Example          string
	Options          []flagInfo
	InheritedOptions []flagInfo
	Children         []commandLink
	Parent           *commandLink
}

type indexPage struct {
	Title        string
	Description  string
	CanonicalURL string
	Active       string
	Nav          []navItem
	Command      *commandInfo
	Root         commandLink
	Commands     []commandLink
}

type sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

func main() {
	outDir := flag.String("dir", "./out/docs/cli-reference", "Output directory")
	flag.Parse()

	if err := generate(*outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(outDir string) error {
	cmd := root.NewCommand()
	cmd.InitDefaultCompletionCmd()
	prepareCommand(cmd)

	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	commands := collectCommands(cmd)
	nav := buildNav(cmd)

	if err := writeFile(outDir, "manual.css", []byte(manualCSS)); err != nil {
		return err
	}

	if err := writeIndex(outDir, indexPage{
		Title:        "Wodby CLI manual",
		Description:  indexDescription,
		CanonicalURL: canonicalURL("index.html"),
		Active:       "index.html",
		Nav:          nav,
		Root:         linkForCommand(cmd),
		Commands:     childLinks(cmd),
	}); err != nil {
		return err
	}

	for _, command := range commands {
		info := infoForCommand(command)
		page := commandPage{
			Title:        info.Path + " | Wodby CLI",
			Description:  commandDescription(info),
			CanonicalURL: canonicalURL(info.File),
			Active:       info.File,
			Nav:          nav,
			Command:      &info,
		}
		if err := writeCommandPage(outDir, page); err != nil {
			return err
		}
	}

	if err := writeSitemap(outDir, commands); err != nil {
		return err
	}

	return nil
}

func prepareCommand(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.InitDefaultHelpFlag()

	for _, child := range cmd.Commands() {
		prepareCommand(child)
	}
}

func collectCommands(cmd *cobra.Command) []*cobra.Command {
	commands := []*cobra.Command{cmd}
	for _, child := range cmd.Commands() {
		if !isDocumentedCommand(child) {
			continue
		}
		commands = append(commands, collectCommands(child)...)
	}
	return commands
}

func buildNav(cmd *cobra.Command) []navItem {
	var items []navItem
	items = append(items, navItem{Link: linkForCommand(cmd)})

	var children []navItem
	for _, child := range cmd.Commands() {
		if !isDocumentedCommand(child) {
			continue
		}
		children = append(children, navForCommand(child))
	}

	return append(items, leavesFirst(children)...)
}

func navForCommand(cmd *cobra.Command) navItem {
	item := navItem{Link: linkForCommand(cmd)}
	for _, child := range cmd.Commands() {
		if !isDocumentedCommand(child) {
			continue
		}
		item.Children = append(item.Children, navForCommand(child))
	}
	item.Children = leavesFirst(item.Children)
	return item
}

func leavesFirst(items []navItem) []navItem {
	if len(items) < 2 {
		return items
	}

	var leaves []navItem
	var branches []navItem
	for _, item := range items {
		if len(item.Children) == 0 {
			leaves = append(leaves, item)
			continue
		}
		branches = append(branches, item)
	}

	return append(leaves, branches...)
}

func infoForCommand(cmd *cobra.Command) commandInfo {
	info := commandInfo{
		Path:             cmd.CommandPath(),
		Name:             cmd.Name(),
		File:             fileName(cmd),
		Short:            cmd.Short,
		Long:             strings.TrimSpace(cmd.Long),
		Usage:            cmd.UseLine(),
		Aliases:          cmd.Aliases,
		Example:          strings.TrimSpace(cmd.Example),
		Options:          flagsForSet(cmd.NonInheritedFlags()),
		InheritedOptions: flagsForSet(cmd.InheritedFlags()),
		Children:         childLinks(cmd),
	}

	if info.Long == info.Short {
		info.Long = ""
	}

	if cmd.HasParent() {
		parent := linkForCommand(cmd.Parent())
		info.Parent = &parent
	}

	return info
}

func childLinks(cmd *cobra.Command) []commandLink {
	var links []commandLink
	for _, child := range cmd.Commands() {
		if !isDocumentedCommand(child) {
			continue
		}
		links = append(links, linkForCommand(child))
	}
	return links
}

func linkForCommand(cmd *cobra.Command) commandLink {
	return commandLink{
		Path:  cmd.CommandPath(),
		Name:  cmd.Name(),
		File:  fileName(cmd),
		Short: cmd.Short,
	}
}

func flagsForSet(flags *pflag.FlagSet) []flagInfo {
	var result []flagInfo
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		if flag.Deprecated != "" {
			return
		}

		info := flagInfo{
			Name:       flagName(flag),
			Usage:      flag.Usage,
			Default:    flag.DefValue,
			HasDefault: flag.DefValue != "",
		}
		result = append(result, info)
	})
	return result
}

func flagName(flag *pflag.Flag) string {
	var parts []string
	if flag.Shorthand != "" {
		parts = append(parts, "-"+flag.Shorthand)
	}
	parts = append(parts, "--"+flag.Name)

	if flag.Value.Type() != "bool" {
		parts[len(parts)-1] += " " + flag.Value.Type()
	}

	return strings.Join(parts, ", ")
}

func isDocumentedCommand(cmd *cobra.Command) bool {
	return cmd.IsAvailableCommand() && !cmd.IsAdditionalHelpTopicCommand()
}

func fileName(cmd *cobra.Command) string {
	return strings.ReplaceAll(cmd.CommandPath(), " ", "_") + ".html"
}

func canonicalURL(file string) string {
	if file == "" || file == "index.html" {
		return cliReferenceBaseURL
	}
	return cliReferenceBaseURL + file
}

func navExpanded(item navItem, active string) bool {
	if item.Link.File == active {
		return true
	}

	for _, child := range item.Children {
		if child.Link.File == active || navExpanded(child, active) {
			return true
		}
	}

	return false
}

func commandDescription(info commandInfo) string {
	summary := strings.TrimSpace(info.Short)
	if summary == "" {
		summary = "Usage, options, aliases, and examples."
	} else if !strings.HasSuffix(summary, ".") {
		summary += "."
	}

	return fmt.Sprintf("%s command reference for Wodby CLI. %s", info.Path, summary)
}

func writeIndex(outDir string, page indexPage) error {
	var buf bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&buf, "index", page); err != nil {
		return err
	}
	return writeFile(outDir, "index.html", buf.Bytes())
}

func writeCommandPage(outDir string, page commandPage) error {
	var buf bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&buf, "command", page); err != nil {
		return err
	}
	return writeFile(outDir, page.Command.File, buf.Bytes())
}

func writeSitemap(outDir string, commands []*cobra.Command) error {
	urls := []sitemapURL{{Loc: canonicalURL("index.html")}}
	for _, command := range commands {
		urls = append(urls, sitemapURL{Loc: canonicalURL(fileName(command))})
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")
	if err := encoder.Encode(sitemap{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}); err != nil {
		return err
	}
	buf.WriteByte('\n')

	return writeFile(outDir, "sitemap.xml", buf.Bytes())
}

func writeFile(outDir string, name string, content []byte) error {
	return os.WriteFile(filepath.Join(outDir, name), content, 0644)
}

var pageTemplate = template.Must(template.New("manual").Funcs(template.FuncMap{
	"dict": func(values ...interface{}) (map[string]interface{}, error) {
		if len(values)%2 != 0 {
			return nil, fmt.Errorf("dict expects key/value pairs")
		}
		dict := make(map[string]interface{}, len(values)/2)
		for i := 0; i < len(values); i += 2 {
			key, ok := values[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict keys must be strings")
			}
			dict[key] = values[i+1]
		}
		return dict, nil
	},
	"navExpanded": navExpanded,
}).Parse(`{{define "layout"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <meta name="description" content="{{.Description}}">
  <link rel="canonical" href="{{.CanonicalURL}}">
  <link rel="icon" href="` + faviconPath + `">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="` + siteName + `">
  <meta property="og:title" content="{{.Title}}">
  <meta property="og:description" content="{{.Description}}">
  <meta property="og:url" content="{{.CanonicalURL}}">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="{{.Title}}">
  <meta name="twitter:description" content="{{.Description}}">
  <link rel="sitemap" type="application/xml" href="` + cliReferenceBaseURL + `sitemap.xml">
  <link rel="stylesheet" href="manual.css">
</head>
<body>
  <header class="topbar">
    <a class="brand" href="index.html">Wodby CLI</a>
    <nav class="topnav" aria-label="Primary">
      <a href="index.html">Manual</a>
      <a href="wodby.html">Commands</a>
      <a href="https://wodby.com/docs/2.0/dev/cli/">Docs</a>
    </nav>
  </header>
  <div class="shell">
    <aside class="sidebar">
      <div class="sidebar-title">CLI manual</div>
      <a class="nav-link {{if eq .Active "index.html"}}active{{end}}" href="index.html">Getting started</a>
      <nav class="command-nav" aria-label="Commands">
        {{template "navItems" .}}
      </nav>
    </aside>
    <main class="content">
      {{template "content" .}}
    </main>
  </div>
</body>
</html>
{{end}}

{{define "navItems"}}
  {{range .Nav}}
    {{template "navItem" dict "Item" . "Active" $.Active}}
  {{end}}
{{end}}

{{define "navItem"}}
  {{$expanded := navExpanded .Item .Active}}
  <div class="nav-item">
    {{if .Item.Children}}
      <details class="nav-branch" data-nav-command="{{.Item.Link.Path}}"{{if $expanded}} open{{end}}>
        <summary class="nav-summary{{if eq .Active .Item.Link.File}} active{{end}}">{{.Item.Link.Name}}</summary>
        <div class="nav-children">
          <a class="nav-link nav-overview{{if eq .Active .Item.Link.File}} active{{end}}" href="{{.Item.Link.File}}">Overview</a>
        {{range .Item.Children}}
          {{template "navItem" dict "Item" . "Active" $.Active}}
        {{end}}
        </div>
      </details>
    {{else}}
      <a class="nav-link {{if eq .Active .Item.Link.File}}active{{end}}" href="{{.Item.Link.File}}">{{.Item.Link.Name}}</a>
    {{end}}
  </div>
{{end}}

{{define "index"}}
  {{template "layout" .}}
{{end}}

{{define "command"}}
  {{template "layout" .}}
{{end}}

{{define "content"}}
  {{if .Command}}
    {{template "commandContent" .Command}}
  {{else}}
    {{template "indexContent" .}}
  {{end}}
{{end}}

{{define "indexContent"}}
  <section class="hero">
    <p class="eyebrow">CLI manual</p>
    <h1>Wodby CLI manual</h1>
    <p class="lead">Command-line reference for Wodby 2.0.</p>
  </section>

  <section class="section">
    <h2>Getting started</h2>
    <p>Set your Wodby API key before running commands:</p>
    <pre><code>export WODBY_API_KEY=...</code></pre>
  </section>

  <section class="section">
    <h2>Available commands</h2>
    <div class="command-grid">
      <a class="command-card" href="{{.Root.File}}">
        <code>{{.Root.Path}}</code>
        <span>{{.Root.Short}}</span>
      </a>
      {{range .Commands}}
        <a class="command-card" href="{{.File}}">
          <code>{{.Path}}</code>
          <span>{{.Short}}</span>
        </a>
      {{end}}
    </div>
  </section>
{{end}}

{{define "commandContent"}}
  <article class="manual-page">
    <div class="breadcrumbs">
      <a href="index.html">Manual</a>
      {{if .Parent}}<span>/</span><a href="{{.Parent.File}}">{{.Parent.Path}}</a>{{end}}
    </div>

    <h1>{{.Path}}</h1>
    {{if .Short}}<p class="lead">{{.Short}}</p>{{end}}
    {{if .Long}}<p>{{.Long}}</p>{{end}}

    {{if .Children}}
      <section class="section">
        <h2>Commands</h2>
        <div class="command-list">
          {{range .Children}}
            <a class="command-row" href="{{.File}}">
              <code>{{.Path}}</code>
              <span>{{.Short}}</span>
            </a>
          {{end}}
        </div>
      </section>
    {{end}}

    <section class="section">
      <h2>Usage</h2>
      <pre><code>{{.Usage}}</code></pre>
    </section>

    {{if .Aliases}}
      <section class="section">
        <h2>Aliases</h2>
        <p>{{range $index, $alias := .Aliases}}{{if $index}}, {{end}}<code>{{$alias}}</code>{{end}}</p>
      </section>
    {{end}}

    {{if .Example}}
      <section class="section">
        <h2>Examples</h2>
        <pre><code>{{.Example}}</code></pre>
      </section>
    {{end}}

    {{if .Options}}
      <section class="section">
        <h2>Options</h2>
        {{template "flagTable" .Options}}
      </section>
    {{end}}

    {{if .InheritedOptions}}
      <section class="section">
        <h2>Options inherited from parent commands</h2>
        {{template "flagTable" .InheritedOptions}}
      </section>
    {{end}}

    {{if .Parent}}
      <section class="section">
        <h2>See also</h2>
        <a href="{{.Parent.File}}">{{.Parent.Path}}</a>
      </section>
    {{end}}
  </article>
{{end}}

{{define "flagTable"}}
  <div class="table-wrap">
    <table>
      <thead>
        <tr>
          <th>Flag</th>
          <th>Description</th>
          <th>Default</th>
        </tr>
      </thead>
      <tbody>
        {{range .}}
          <tr>
            <td><code>{{.Name}}</code></td>
            <td>{{.Usage}}</td>
            <td>{{if .HasDefault}}<code>{{.Default}}</code>{{else}}<span class="muted">-</span>{{end}}</td>
          </tr>
        {{end}}
      </tbody>
    </table>
  </div>
{{end}}
`))

const manualCSS = `:root {
  color-scheme: light;
  --bg: #ffffff;
  --panel: #f6f8fa;
  --panel-border: #d8dee4;
  --text: #24292f;
  --muted: #57606a;
  --link: #0969da;
  --link-hover: #0550ae;
  --code-bg: #f6f8fa;
  --accent: #2da44e;
  --shadow: 0 1px 2px rgba(27, 31, 36, 0.06);
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 16px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
}

a {
  color: var(--link);
  text-decoration: none;
}

a:hover {
  color: var(--link-hover);
  text-decoration: underline;
}

.topbar {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 64px;
  padding: 0 28px;
  background: #24292f;
  color: #ffffff;
}

.brand {
  color: #ffffff;
  font-weight: 700;
  font-size: 18px;
}

.brand:hover,
.topnav a:hover {
  color: #ffffff;
}

.topnav {
  display: flex;
  gap: 18px;
}

.topnav a {
  color: rgba(255, 255, 255, 0.86);
  font-size: 14px;
}

.shell {
  display: grid;
  grid-template-columns: 288px minmax(0, 1fr);
  min-height: calc(100vh - 64px);
}

.sidebar {
  position: sticky;
  top: 64px;
  align-self: start;
  height: calc(100vh - 64px);
  overflow-y: auto;
  padding: 24px 20px;
  border-right: 1px solid var(--panel-border);
  background: var(--panel);
}

.sidebar-title {
  margin: 0 0 14px;
  color: var(--muted);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.command-nav {
  margin-top: 12px;
}

.nav-item {
  margin: 1px 0;
}

.nav-branch {
  margin: 1px 0;
}

.nav-summary {
  padding: 5px 8px;
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font-size: 14px;
  overflow-wrap: anywhere;
}

.nav-summary::marker {
  color: var(--muted);
}

.nav-summary:hover {
  background: #eaeef2;
}

.nav-summary.active {
  background: #ddf4ff;
  color: #0969da;
  font-weight: 600;
}

.nav-children {
  margin-left: 14px;
  padding-left: 10px;
  border-left: 1px solid #d0d7de;
}

.nav-link {
  display: block;
  padding: 5px 8px;
  border-radius: 6px;
  color: var(--text);
  font-size: 14px;
  overflow-wrap: anywhere;
}

.nav-overview {
  color: var(--muted);
}

.nav-link:hover {
  background: #eaeef2;
  text-decoration: none;
}

.nav-link.active {
  background: #ddf4ff;
  color: #0969da;
  font-weight: 600;
}

.content {
  width: min(100%, 980px);
  padding: 48px 56px 72px;
}

.hero {
  padding-bottom: 24px;
  border-bottom: 1px solid var(--panel-border);
}

.eyebrow {
  margin: 0 0 8px;
  color: var(--accent);
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}

h1 {
  margin: 0 0 10px;
  font-size: 42px;
  line-height: 1.18;
  letter-spacing: 0;
}

h2 {
  margin: 0 0 14px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--panel-border);
  font-size: 24px;
  line-height: 1.3;
  letter-spacing: 0;
}

p {
  margin: 0 0 14px;
}

.lead {
  color: var(--muted);
  font-size: 19px;
}

.section {
  margin-top: 34px;
}

.breadcrumbs {
  display: flex;
  gap: 8px;
  margin-bottom: 18px;
  color: var(--muted);
  font-size: 14px;
}

pre {
  margin: 0;
  padding: 16px;
  overflow-x: auto;
  border: 1px solid var(--panel-border);
  border-radius: 6px;
  background: var(--code-bg);
  box-shadow: var(--shadow);
}

code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace;
  font-size: 0.92em;
}

:not(pre) > code {
  padding: 0.12em 0.34em;
  border-radius: 4px;
  background: var(--code-bg);
}

.command-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}

.command-card,
.command-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px;
  border: 1px solid var(--panel-border);
  border-radius: 6px;
  background: #ffffff;
  color: var(--text);
  box-shadow: var(--shadow);
}

.command-card:hover,
.command-row:hover {
  border-color: #0969da;
  text-decoration: none;
}

.command-card span,
.command-row span {
  color: var(--muted);
}

.command-list {
  display: grid;
  gap: 10px;
}

.table-wrap {
  overflow-x: auto;
  border: 1px solid var(--panel-border);
  border-radius: 6px;
  box-shadow: var(--shadow);
}

table {
  width: 100%;
  border-collapse: collapse;
}

th,
td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--panel-border);
  text-align: left;
  vertical-align: top;
}

th {
  background: var(--panel);
  font-size: 13px;
}

tr:last-child td {
  border-bottom: 0;
}

.muted {
  color: var(--muted);
}

@media (max-width: 860px) {
  .topbar {
    position: static;
    padding: 16px 20px;
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
  }

  .shell {
    display: block;
  }

  .sidebar {
    position: static;
    height: auto;
    max-height: 360px;
    border-right: 0;
    border-bottom: 1px solid var(--panel-border);
  }

  .content {
    padding: 32px 22px 56px;
  }

  h1 {
    font-size: 32px;
  }
}
`
