package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

//go:embed templates/*.html
var templateFS embed.FS

// templateFuncs are the helpers available to all templates.
var templateFuncs = template.FuncMap{
	"active": func(currentPath, href string) bool {
		return currentPath == href ||
			(href != "/admin" && strings.HasPrefix(currentPath, href+"/"))
	},
	"fmtDate":  formatDateTime,
	"fmtCount": formatCount,
	"orDash":   orDash,
	"deref":    valueOrEmpty,
	"csv": func(s string) []string {
		parts := strings.Split(s, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return parts
	},
}

// pageTemplateNames lists the dashboard pages; each defines "content" and is
// rendered inside the shared layout.
var pageTemplateNames = []string{
	"overview", "shorturls", "shorturl_new", "shorturl_edit",
	"visits_shorturl", "visits_orphan", "tags", "domains", "message",
	"apikeys", "users", "webhooks",
}

// parseTemplates builds the shared template set (layout, partials, the
// standalone login/public pages) and one clone per dashboard page.
func parseTemplates() (base *template.Template, pages map[string]*template.Template) {
	base = template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFS,
		"templates/layout.html", "templates/partials.html",
		"templates/login.html", "templates/public.html"))
	pages = make(map[string]*template.Template, len(pageTemplateNames))
	for _, name := range pageTemplateNames {
		clone := template.Must(base.Clone())
		pages[name] = template.Must(clone.ParseFS(templateFS, "templates/"+name+".html"))
	}
	return base, pages
}

// view is what the layout template receives; Data carries the page's model.
type view struct {
	Title string
	Path  string
	User  *CurrentUser
	Data  any
}

// render executes a template into a buffer first, so a failure part-way
// through never leaks half a page; the handler adapter turns the returned
// error into the 500.
func (a *App) render(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("rendering %s: %w", name, err)
	}
	respondHtml(w, status, buf.String())
	return nil
}

// renderPage renders a dashboard page inside the layout.
func (a *App) renderPage(w http.ResponseWriter, status int, page string, user *CurrentUser, path, title string, data any) error {
	return a.render(w, status, a.pages[page], "layout", view{Title: title, Path: path, User: user, Data: data})
}

// renderShared renders a template from the shared set: the standalone
// login/landing/404 pages and htmx fragments.
func (a *App) renderShared(w http.ResponseWriter, status int, name string, data any) error {
	return a.render(w, status, a.baseTemplates, name, data)
}

func respondHtml(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// redirect is http.Redirect (302) shaped as a handler's final statement.
func redirect(w http.ResponseWriter, r *http.Request, url string) error {
	http.Redirect(w, r, url, http.StatusFound)
	return nil
}

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

// ---- Shared view-model pieces ----

type pagerView struct {
	PrevUrl     string
	NextUrl     string
	CurrentPage int
	TotalPages  int
	TotalItems  int64
}

func newPager[T any](page core.Page[T], buildUrl func(int) string) pagerView {
	total := page.TotalPages()
	if total < 1 {
		total = 1
	}
	pv := pagerView{CurrentPage: page.CurrentPage, TotalPages: total, TotalItems: page.TotalItems}
	if page.CurrentPage > 1 {
		pv.PrevUrl = buildUrl(page.CurrentPage - 1)
	}
	if page.CurrentPage < page.TotalPages() {
		pv.NextUrl = buildUrl(page.CurrentPage + 1)
	}
	return pv
}

type optionView struct {
	Value    string
	Selected bool
}

func optionsOf(values []string, current string) []optionView {
	out := make([]optionView, len(values))
	for i, v := range values {
		out[i] = optionView{Value: v, Selected: v == current}
	}
	return out
}

type groupPickerView struct {
	IsAdmin bool
	Current string
	Options []string
}

func newGroupPicker(user *CurrentUser, current string) groupPickerView {
	return groupPickerView{IsAdmin: user.IsAdmin(), Current: current, Options: user.Groups}
}

type statusOptionView struct {
	Code     int
	Label    string
	Selected bool
}

func statusOptions(current int) []statusOptionView {
	all := []statusOptionView{
		{Code: 301, Label: "301 — permanent"},
		{Code: 302, Label: "302 — found (default)"},
		{Code: 307, Label: "307 — temporary, keep method"},
		{Code: 308, Label: "308 — permanent, keep method"},
	}
	for i := range all {
		all[i].Selected = all[i].Code == current
	}
	return all
}

func orDash(p *string) string {
	if p == nil {
		return "—"
	}
	return *p
}

func valueOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
