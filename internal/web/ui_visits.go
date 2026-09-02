package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func visitTable(showVisitedUrl bool, page core.Page[data.VisitRow], buildUrl func(int) string) h.Node {
	str := func(p *string) string {
		if p == nil {
			return "—"
		}
		return *p
	}

	var rows []h.Node
	for _, v := range page.Items {
		location := "—"
		switch {
		case v.City != nil && v.CountryName != nil:
			location = *v.City + ", " + *v.CountryName
		case v.CountryName != nil:
			location = *v.CountryName
		}
		browserOs := "—"
		switch {
		case v.Browser != nil && v.Os != nil:
			browserOs = *v.Browser + " / " + *v.Os
		case v.Browser != nil:
			browserOs = *v.Browser
		case v.Os != nil:
			browserOs = *v.Os
		}
		referer := h.Node(h.Text("—"))
		if v.Referer != nil {
			referer = h.E("span", []h.Attr{h.A("class", "truncate"), h.A("style", "max-width:220px")}, h.Text(*v.Referer))
		}
		botBadge := h.Empty()
		if v.IsBot {
			botBadge = h.E("span", []h.Attr{h.A("class", "badge red")}, h.Text("bot"))
		}

		cells := []h.Node{
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(formatDateTime(v.VisitedAt))),
		}
		if showVisitedUrl {
			cells = append(cells, h.E("td", nil,
				h.E("span", []h.Attr{h.A("class", "truncate mono"), h.A("style", "max-width:260px")},
					h.Text(str(v.VisitedUrl)))))
		}
		cells = append(cells,
			h.E("td", nil, h.Text(location)),
			h.E("td", nil, h.Text(browserOs)),
			h.E("td", nil, h.Text(str(v.Device))),
			h.E("td", nil, referer),
			h.E("td", nil, botBadge))
		rows = append(rows, h.E("tr", nil, cells...))
	}

	headCells := []h.Node{h.E("th", nil, h.Text("When (UTC)"))}
	if showVisitedUrl {
		headCells = append(headCells, h.E("th", nil, h.Text("Visited URL")))
	}
	headCells = append(headCells,
		h.E("th", nil, h.Text("Location")),
		h.E("th", nil, h.Text("Browser / OS")),
		h.E("th", nil, h.Text("Device")),
		h.E("th", nil, h.Text("Referrer")),
		h.E("th", nil, h.Text("Bot?")))

	return h.E("div", nil,
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil, h.E("tr", nil, headCells...)),
				h.E("tbody", nil, rows...))),
		pager(buildUrl, page))
}

func rangeForm(action, startDate, endDate string) h.Node {
	return h.E("form", []h.Attr{h.A("class", "toolbar"), h.A("method", "get"), h.A("action", action)},
		h.E("div", nil,
			h.E("label", nil, h.Text("From (UTC)")),
			h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "startDate"), h.A("value", startDate)})),
		h.E("div", nil,
			h.E("label", nil, h.Text("To (UTC)")),
			h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "endDate"), h.A("value", endDate)})),
		h.E("button", []h.Attr{h.A("class", "secondary")}, h.Text("Apply")))
}

func timeNowUtcDate() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}

// analyticsContent is the shared analytics block: chart + breakdowns + visit
// table.
func (a *App) analyticsContent(
	showVisitedUrl bool,
	scope data.VisitScope,
	listVisits func(data.VisitFilters) (core.Page[data.VisitRow], error),
	basePath string,
	q url.Values,
) ([]h.Node, error) {
	startDate := queryDate(q, "startDate")
	endDate := queryDate(q, "endDate")
	filters := data.VisitFilters{
		StartDate:    startDate,
		EndDate:      endDate,
		Page:         queryIntDefault(q, "page", 1),
		ItemsPerPage: 25,
	}

	defaultedStart := startDate
	if defaultedStart == nil {
		start := timeNowUtcDate().AddDate(0, 0, -29)
		defaultedStart = &start
	}
	series, err := data.VisitsPerDay(a.Db, scope, defaultedStart, endDate)
	if err != nil {
		return nil, err
	}

	card := func(title, column string) (h.Node, error) {
		rows, err := data.Breakdown(a.Db, scope, column, startDate, endDate, 8)
		if err != nil {
			return h.Empty(), err
		}
		return h.E("div", []h.Attr{h.A("class", "card")},
			h.E("h2", []h.Attr{h.A("style", "margin-top:0")}, h.Text(title)),
			chartBarList(rows)), nil
	}
	byCountry, err := card("Countries", "country_name")
	if err != nil {
		return nil, err
	}
	byBrowser, err := card("Browsers", "browser")
	if err != nil {
		return nil, err
	}
	byOs, err := card("Operating systems", "os")
	if err != nil {
		return nil, err
	}
	byReferer, err := card("Referrers", "referer")
	if err != nil {
		return nil, err
	}

	page, err := listVisits(filters)
	if err != nil {
		return nil, err
	}

	buildUrl := func(p int) string {
		var parts []string
		if s := q.Get("startDate"); s != "" {
			parts = append(parts, "startDate="+url.QueryEscape(s))
		}
		if s := q.Get("endDate"); s != "" {
			parts = append(parts, "endDate="+url.QueryEscape(s))
		}
		if p > 1 {
			parts = append(parts, fmt.Sprintf("page=%d", p))
		}
		if len(parts) == 0 {
			return basePath
		}
		return basePath + "?" + strings.Join(parts, "&")
	}

	return []h.Node{
		rangeForm(basePath, q.Get("startDate"), q.Get("endDate")),
		h.E("div", []h.Attr{h.A("class", "card chart-card")},
			h.E("h2", []h.Attr{h.A("style", "margin-top:0")}, h.Text("Visits per day")),
			chartVisitsPerDay(series)),
		h.E("div", []h.Attr{h.A("class", "split")}, byCountry, byBrowser, byOs, byReferer),
		h.E("h2", nil, h.Text("Visits")),
		visitTable(showVisitedUrl, page, buildUrl),
	}, nil
}

// GET /admin/short-urls/{id}/visits
func (a *App) uiShortUrlVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondPlainNotFound(w)
		return
	}
	detail, err := data.TryGetDetailById(a.Db, core.ShortUrlID(id))
	if err != nil {
		a.serverError(w, err)
		return
	}
	if detail == nil || !user.CanSeeGroup(detail.GroupName) {
		respondPlainNotFound(w)
		return
	}
	q := r.URL.Query()
	shortUrlId := core.ShortUrlID(detail.Id)
	content, err := a.analyticsContent(false, data.ShortUrlScope(shortUrlId),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListVisitsForShortUrl(a.Db, shortUrlId, f)
		},
		fmt.Sprintf("/admin/short-urls/%d/visits", detail.Id), q)
	if err != nil {
		a.serverError(w, err)
		return
	}

	header := []h.Node{
		h.E("h1", nil,
			h.Text("Visits — "),
			h.E("span", []h.Attr{h.A("class", "mono")}, h.Text(detail.Authority+"/"+detail.ShortCode))),
		h.E("p", nil,
			h.E("a", []h.Attr{h.A("href", fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id))},
				h.Text("← Back to edit")),
			h.Text(" · "),
			h.E("a", []h.Attr{
				h.A("href", ShortUrlFor(a.Cfg, detail.Authority, detail.ShortCode)),
				h.A("target", "_blank"), h.A("rel", "noreferrer"),
			}, h.Text(detail.LongUrl))),
	}
	respondPage(w, user, "/admin/short-urls", "Visits", append(header, content...))
}

// GET /admin/visits/orphan
func (a *App) uiOrphanVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(q.Get("type")); ok {
		visitType = &vt
	}
	content, err := a.analyticsContent(true, data.OrphanScope(),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListOrphanVisits(a.Db, visitType, f)
		},
		"/admin/visits/orphan", q)
	if err != nil {
		a.serverError(w, err)
		return
	}

	deleteForm := h.Empty()
	if user.IsAdmin() {
		deleteForm = h.E("form", []h.Attr{
			h.A("class", "inline"), h.A("method", "post"),
			h.A("action", "/admin/visits/orphan/delete"),
			h.A("onsubmit", "return confirm('Delete ALL orphan visits?')"),
		},
			h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete all orphan visits")))
	}

	header := []h.Node{
		h.E("h1", nil, h.Text("Orphan visits")),
		h.E("p", []h.Attr{h.A("class", "muted")},
			h.Text("Traffic that reached this server without hitting an active short URL: base URL hits, unknown short codes and other 404s.")),
		h.E("div", []h.Attr{h.A("class", "toolbar")},
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", "/admin/visits/orphan")}, h.Text("All")),
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", "/admin/visits/orphan?type=base_url")}, h.Text("Base URL")),
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", "/admin/visits/orphan?type=invalid_short_url")}, h.Text("Invalid short URLs")),
			h.E("a", []h.Attr{h.A("class", "btn secondary small"), h.A("href", "/admin/visits/orphan?type=regular_404")}, h.Text("Other 404s")),
			deleteForm),
	}
	respondPage(w, user, "/admin/visits/orphan", "Orphan visits", append(header, content...))
}

// POST /admin/visits/orphan/delete (admin)
func (a *App) uiDeleteOrphanVisits(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if _, err := data.DeleteOrphanVisits(a.Db); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/visits/orphan", http.StatusFound)
}
