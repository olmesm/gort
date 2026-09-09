package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
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
	ShowVisitedURL bool
	Visits         []visitRowView
	Pager          pagerView
}

type breakdownCardView struct {
	Title string
	Rows  []barRowView
}

type visitRowView struct {
	When       string
	VisitedURL string
	Location   string
	BrowserOS  string
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
	browserOS := "—"
	switch {
	case v.Browser != nil && v.OS != nil:
		browserOS = *v.Browser + " / " + *v.OS
	case v.Browser != nil:
		browserOS = *v.Browser
	case v.OS != nil:
		browserOS = *v.OS
	}
	return visitRowView{
		When:       formatDateTime(v.VisitedAt),
		VisitedURL: orDash(v.VisitedURL),
		Location:   location,
		BrowserOS:  browserOS,
		Device:     orDash(v.Device),
		Referer:    v.Referer,
		IsBot:      v.IsBot,
	}
}

// analyticsContent is the shared analytics block: chart + breakdowns + visit
// table.
func (a *App) analyticsContent(
	ctx context.Context,
	showVisitedURL bool,
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
	series, err := data.VisitsPerDay(ctx, a.DB, scope, defaultedStart, endDate)
	if err != nil {
		return analyticsView{}, err
	}

	model := analyticsView{
		BasePath:       basePath,
		StartVal:       q.Get("startDate"),
		EndVal:         q.Get("endDate"),
		Chart:          chartVisitsPerDay(series),
		ShowVisitedURL: showVisitedURL,
	}

	for _, card := range []struct{ title, column string }{
		{"Countries", "country_name"},
		{"Browsers", "browser"},
		{"Operating systems", "os"},
		{"Referrers", "referer"},
	} {
		rows, err := data.Breakdown(ctx, a.DB, scope, card.column, startDate, endDate, 8)
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

type shortURLVisitsView struct {
	Display   string
	EditURL   string
	ShortURL  string
	LongURL   string
	Analytics analyticsView
}

// GET /admin/short-urls/{id}/visits
func (a *App) uiShortURLVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	id, err := pathID[core.ShortURLID](r, "id")
	if err != nil {
		return err
	}
	detail, err := data.ShortURLDetailByID(r.Context(), a.DB, id)
	if err != nil {
		return err
	}
	if detail == nil || !user.CanSeeGroup(detail.GroupName) {
		return errPageNotFound
	}
	shortURLID := detail.ID
	analytics, err := a.analyticsContent(r.Context(), false, data.ShortURLScope(shortURLID),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListVisitsForShortURL(r.Context(), a.DB, shortURLID, f)
		},
		fmt.Sprintf("/admin/short-urls/%d/visits", detail.ID), r.URL.Query())
	if err != nil {
		return err
	}

	a.renderPage(w, http.StatusOK, "visits_shorturl", user, "/admin/short-urls", "Visits", shortURLVisitsView{
		Display:   detail.Authority + "/" + detail.ShortCode,
		EditURL:   fmt.Sprintf("/admin/short-urls/%d/edit", detail.ID),
		ShortURL:  ShortURLFor(a.Cfg, detail.Authority, detail.ShortCode),
		LongURL:   detail.LongURL,
		Analytics: analytics,
	})
	return nil
}

type orphanVisitsView struct {
	ShowDelete bool
	Analytics  analyticsView
}

// GET /admin/visits/orphan
func (a *App) uiOrphanVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	var visitType *core.VisitType
	if vt, ok := core.VisitTypeOfSlug(q.Get("type")); ok {
		visitType = &vt
	}
	analytics, err := a.analyticsContent(r.Context(), true, data.OrphanScope(),
		func(f data.VisitFilters) (core.Page[data.VisitRow], error) {
			return data.ListOrphanVisits(r.Context(), a.DB, visitType, f)
		},
		"/admin/visits/orphan", q)
	if err != nil {
		return err
	}
	a.renderPage(w, http.StatusOK, "visits_orphan", user, "/admin/visits/orphan", "Orphan visits", orphanVisitsView{
		ShowDelete: user.IsAdmin(),
		Analytics:  analytics,
	})
	return nil
}

// POST /admin/visits/orphan/delete (admin)
func (a *App) uiDeleteOrphanVisits(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if _, err := data.DeleteOrphanVisits(r.Context(), a.DB); err != nil {
		return err
	}
	return redirect(w, r, "/admin/visits/orphan")
}
