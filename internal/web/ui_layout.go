package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/h"
)

// ---- htmx attribute helpers and request detection ----

func hxGet(url string) h.Attr         { return h.A("hx-get", url) }
func hxTarget(sel string) h.Attr      { return h.A("hx-target", sel) }
func hxSwap(mode string) h.Attr       { return h.A("hx-swap", mode) }
func hxTrigger(trigger string) h.Attr { return h.A("hx-trigger", trigger) }
func hxPushUrl() h.Attr               { return h.A("hx-push-url", "true") }

// isHtmx: was this request issued by htmx (so we should render a fragment)?
func isHtmx(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// ---- Formatting ----

func formatDateTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") }

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// ---- Layout ----

func navLink(currentPath, href, label string) h.Node {
	isActive := currentPath == href ||
		(href != "/admin" && strings.HasPrefix(currentPath, href+"/")) ||
		(href != "/admin" && currentPath == href)
	return h.E("a", []h.Attr{
		h.A("href", href),
		h.If(isActive || (href == "/admin" && currentPath == "/admin"), h.A("class", "active")),
	}, h.Text(label))
}

// layoutPage is the full dashboard page shell.
func layoutPage(user *CurrentUser, currentPath, title string, content []h.Node) string {
	return h.Document(h.E("html", []h.Attr{h.A("lang", "en")},
		h.E("head", nil,
			h.E("meta", []h.Attr{h.A("charset", "utf-8")}),
			h.E("meta", []h.Attr{h.A("name", "viewport"), h.A("content", "width=device-width, initial-scale=1")}),
			h.E("title", nil, h.Text(title+" · Gort")),
			h.E("link", []h.Attr{h.A("rel", "stylesheet"), h.A("href", "/app.css")}),
			h.E("script", []h.Attr{h.A("src", "/htmx.min.js"), h.Flag("defer")})),
		h.E("body", nil,
			h.E("header", []h.Attr{h.A("class", "topbar")},
				h.E("a", []h.Attr{h.A("class", "brand"), h.A("href", "/admin")}, h.Text("Gort")),
				h.E("nav", nil,
					navLink(currentPath, "/admin", "Overview"),
					navLink(currentPath, "/admin/short-urls", "Short URLs"),
					navLink(currentPath, "/admin/tags", "Tags"),
					navLink(currentPath, "/admin/domains", "Domains"),
					navLink(currentPath, "/admin/visits/orphan", "Orphan visits"),
					h.IfNode(user.IsAdmin(), navLink(currentPath, "/admin/api-keys", "API keys")),
					h.IfNode(user.IsAdmin(), navLink(currentPath, "/admin/webhooks", "Webhooks")),
					h.IfNode(user.IsAdmin(), navLink(currentPath, "/admin/users", "Users"))),
				h.E("div", []h.Attr{h.A("class", "spacer")}),
				h.E("span", []h.Attr{h.A("class", "who")}, h.Text(user.Username)),
				h.E("form", []h.Attr{h.A("class", "inline"), h.A("method", "post"), h.A("action", "/admin/logout")},
					h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text("Log out")))),
			h.E("main", []h.Attr{h.A("class", "container")}, content...))))
}

// layoutBare is a minimal shell for unauthenticated pages (login).
func layoutBare(title string, content []h.Node) string {
	return h.Document(h.E("html", []h.Attr{h.A("lang", "en")},
		h.E("head", nil,
			h.E("meta", []h.Attr{h.A("charset", "utf-8")}),
			h.E("meta", []h.Attr{h.A("name", "viewport"), h.A("content", "width=device-width, initial-scale=1")}),
			h.E("title", nil, h.Text(title+" · Gort")),
			h.E("link", []h.Attr{h.A("rel", "stylesheet"), h.A("href", "/app.css")})),
		h.E("body", nil, content...)))
}

func respondHtml(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func respondPage(w http.ResponseWriter, user *CurrentUser, currentPath, title string, content []h.Node) {
	respondHtml(w, http.StatusOK, layoutPage(user, currentPath, title, content))
}

// respondFragment renders a node as an htmx fragment.
func respondFragment(w http.ResponseWriter, node h.Node) {
	respondHtml(w, http.StatusOK, h.Render(node))
}

// ---- Small shared building blocks ----

func alertError(message string) h.Node {
	return h.E("div", []h.Attr{h.A("class", "alert error")}, h.Text(message))
}

func alertSuccess(nodes ...h.Node) h.Node {
	return h.E("div", []h.Attr{h.A("class", "alert success")}, nodes...)
}

func formField(labelText string, input h.Node) h.Node {
	return h.E("div", nil, h.E("label", nil, h.Text(labelText)), input)
}

func textInput(name, value, placeholder string) h.Node {
	return h.E("input", []h.Attr{
		h.A("type", "text"),
		h.A("name", name),
		h.A("value", value),
		h.If(placeholder != "", h.A("placeholder", placeholder)),
	})
}

func checkbox(name string, isChecked bool, labelText string) h.Node {
	return h.E("div", []h.Attr{h.A("class", "checkbox")},
		h.E("input", []h.Attr{
			h.A("type", "checkbox"),
			h.A("name", name),
			h.A("id", name),
			h.A("value", "true"),
			h.If(isChecked, h.Flag("checked")),
		}),
		h.E("label", []h.Attr{h.A("for", name)}, h.Text(labelText)))
}

// pager renders pagination controls that navigate via query params on the
// same path.
func pager[T any](buildUrl func(int) string, page core.Page[T]) h.Node {
	totalPages := page.TotalPages()
	if totalPages < 1 {
		totalPages = 1
	}
	return h.E("div", []h.Attr{h.A("class", "pager")},
		h.IfNode(page.CurrentPage > 1,
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", buildUrl(page.CurrentPage-1))},
				h.Text("← Prev"))),
		h.E("span", []h.Attr{h.A("class", "info")},
			h.Text(fmt.Sprintf("Page %d of %d · %d items", page.CurrentPage, totalPages, page.TotalItems))),
		h.IfNode(page.CurrentPage < page.TotalPages(),
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", buildUrl(page.CurrentPage+1))},
				h.Text("Next →"))))
}
