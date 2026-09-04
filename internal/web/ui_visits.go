package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type analyticsView struct {
	BasePath       string
	StartVal       string
	EndVal         string
	Chart          template.HTML
	Breakdowns     []breakdownCardView
	ShowVisitedUrl bool
	Visits         []visitRowView
	Pager          pagerView
}

type breakdownCardView struct {
	Title string
	Rows  []barRowView
}

type visitRowView struct {
	When       string
	VisitedUrl string
	Location   string
	BrowserOs  string
	Device     string
	Referer    *string
	IsBot      bool
}

func newVisitRow(v data.VisitRow) visitRowView {
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
	return visitRowView{
		When:       formatDateTime(v.VisitedAt),
		VisitedUrl: orDash(v.VisitedUrl),
		Location:   location,
		BrowserOs:  browserOs,
		Device:     orDash(v.Device),
		Referer:    v.Referer,
		IsBot:      v.IsBot,
	}
}

// analyticsContent is the shared analytics block: chart + breakdowns + visit
// table.
func (a *App) analyticsContent(
	showVisitedUrl bool,
	scope data.VisitScope,
	listVisits func(data.VisitFilters) (core.Page[data.VisitRow], error),
	basePath string,
	q url.Values,
) (analyticsView, error) {
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
		start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -29)
		defaultedStart = &start
	}
	series, err := data.VisitsPerDay(a.Db, scope, defaultedStart, endDate)
	if err != nil {
		return analyticsView{}, err
	}

	model := analyticsView{
		BasePath:       basePath,
		StartVal:       q.Get("startDate"),
		EndVal:         q.Get("endDate"),
		Chart:          chartVisitsPerDay(series),
		ShowVisitedUrl: showVisitedUrl,
	}

	for _, card := range []struct{ title, column string }{
		{"Countries", "country_name"},
		{"Browsers", "browser"},
		{"Operating systems", "os"},
		{"Referrers", "referer"},
	} {
		rows, err := data.Breakdown(a.Db, scope, card.column, startDate, endDate, 8)
		if err != nil {
			return analyticsView{}, err
		}
		model.Breakdowns = append(model.Breakdowns, breakdownCardView{Title: card.title, Rows: barRows(rows)})
	}

	page, err := listVisits(filters)
	if err != nil {
		return analyticsView{}, err
	}
	for _, v := range page.Items {
		model.Visits = append(model.Visits, newVisitRow(v))
	}
	model.Pager = newPager(page, func(p int) string {
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
	})
	return model, nil
}

type shortUrlVisitsView struct {
	Display   string
	EditUrl   string
	ShortUrl  string
	LongUrl   string
	Analytics analyticsView
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
	shortUrlId := core.ShortUrlID(detail.Id)
	analytics, err := a.analyticsContent(false, data.ShortUrlScope(shortUrlId),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListVisitsForShortUrl(a.Db, shortUrlId, f)
		},
		fmt.Sprintf("/admin/short-urls/%d/visits", detail.Id), r.URL.Query())
	if err != nil {
		a.serverError(w, err)
		return
	}

	a.renderPage(w, http.StatusOK, "visits_shorturl", user, "/admin/short-urls", "Visits", shortUrlVisitsView{
		Display:   detail.Authority + "/" + detail.ShortCode,
		EditUrl:   fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id),
		ShortUrl:  ShortUrlFor(a.Cfg, detail.Authority, detail.ShortCode),
		LongUrl:   detail.LongUrl,
		Analytics: analytics,
	})
}

type orphanVisitsView struct {
	ShowDelete bool
	Analytics  analyticsView
}

// GET /admin/visits/orphan
func (a *App) uiOrphanVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(q.Get("type")); ok {
		visitType = &vt
	}
	analytics, err := a.analyticsContent(true, data.OrphanScope(),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListOrphanVisits(a.Db, visitType, f)
		},
		"/admin/visits/orphan", q)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.renderPage(w, http.StatusOK, "visits_orphan", user, "/admin/visits/orphan", "Orphan visits", orphanVisitsView{
		ShowDelete: user.IsAdmin(),
		Analytics:  analytics,
	})
}

// POST /admin/visits/orphan/delete (admin)
func (a *App) uiDeleteOrphanVisits(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if _, err := data.DeleteOrphanVisits(a.Db); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/visits/orphan", http.StatusFound)
}
