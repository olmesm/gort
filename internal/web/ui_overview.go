package web

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/olmesm/gort/internal/data"
)

type overviewView struct {
	GeoWarning bool
	Stats      data.OverviewRow
	Chart      template.HTML
	Recent     []overviewRecentRow
}

type overviewRecentRow struct {
	EditUrl string
	Display string
	LongUrl string
	Visits  int64
	Created string
}

// GET /admin — dashboard overview.
func (a *App) uiOverview(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	stats, err := data.Overview(r.Context(), a.Db)
	if err != nil {
		return err
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -29)
	series, err := data.VisitsPerDay(r.Context(), a.Db, data.GlobalScope(), &start, nil)
	if err != nil {
		return err
	}
	recentFilters := data.EmptyShortUrlFilters()
	recentFilters.ItemsPerPage = 5
	recentFilters.VisibleGroups = user.VisibleGroups()
	recent, err := data.ListShortUrls(r.Context(), a.Db, recentFilters)
	if err != nil {
		return err
	}

	model := overviewView{
		GeoWarning: !(a.Cfg.DisableTracking || a.Geo.IsAvailable() || a.Cfg.GeoLiteLicenseKey != ""),
		Stats:      stats,
		Chart:      chartVisitsPerDay(series),
	}
	for _, d := range recent.Items {
		model.Recent = append(model.Recent, overviewRecentRow{
			EditUrl: fmt.Sprintf("/admin/short-urls/%d/edit", d.Id),
			Display: d.Authority + "/" + d.ShortCode,
			LongUrl: d.LongUrl,
			Visits:  d.VisitCount,
			Created: formatDateTime(d.CreatedAt),
		})
	}
	return a.renderPage(w, http.StatusOK, "overview", user, "/admin", "Overview", model)
}
