package htmltext

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// ToEmailText preserves readable paragraphs and safe links without web-only controls.
func ToEmailText(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return StripToPlainText(raw)
	}
	var hasMarkup func(*html.Node) bool
	hasMarkup = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data != "html" && n.Data != "head" && n.Data != "body" {
			return true
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if hasMarkup(child) {
				return true
			}
		}
		return false
	}
	if !hasMarkup(doc) {
		return normalizeEmailLines(raw)
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "button", "noscript", "iframe", "template":
				return
			}
			for _, attr := range n.Attr {
				if attr.Key == "data-copy-text" {
					return
				}
			}
		}
		block := n.Type == html.ElementNode && strings.Contains("|p|div|section|article|h1|h2|h3|h4|h5|h6|ul|ol|li|tr|hr|br|", "|"+n.Data+"|")
		if block && !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
		if n.Type == html.ElementNode && n.Data == "li" {
			out.WriteString("• ")
		}
		if n.Type == html.TextNode {
			out.WriteString(strings.ReplaceAll(n.Data, "\n", " "))
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key != "href" {
					continue
				}
				href := strings.TrimSpace(attr.Val)
				u, err := url.Parse(href)
				if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
					out.WriteString(" (" + href + ")")
				}
			}
		}
		if block {
			out.WriteByte('\n')
		}
		if n.Type == html.ElementNode && n.Data == "p" {
			out.WriteByte('\n')
		}
	}
	walk(doc)
	return normalizeEmailLines(out.String())
}

func normalizeEmailLines(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" || (len(lines) > 0 && lines[len(lines)-1] != "") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
