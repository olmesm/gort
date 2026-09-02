package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func statTile(label, value string, href string) h.Node {
	labelNode := h.Text(label)
	if href != "" {
		labelNode = h.E("a", []h.Attr{h.A("href", href)}, h.Text(label))
	}
	return h.E("div", []h.Attr{h.A("class", "stat")},
		h.E("div", []h.Attr{h.A("class", "num")}, h.Text(value)),
		h.E("div", []h.Attr{h.A("class", "label")}, labelNode))
}

// GET /admin — dashboard overview.
func (a *App) uiOverview(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	o, err := data.Overview(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -29)
	series, err := data.VisitsPerDay(a.Db, data.GlobalScope(), &start, nil)
	if err != nil {
		a.serverError(w, err)
		return
	}
	recentFilters := data.EmptyShortUrlFilters()
	recentFilters.ItemsPerPage = 5
	recentFilters.VisibleGroups = user.VisibleGroups()
	recent, err := data.ListShortUrls(a.Db, recentFilters)
	if err != nil {
		a.serverError(w, err)
		return
	}

	geoWarning := h.Empty()
	if !(a.Cfg.DisableTracking || a.Geo.IsAvailable() || a.Cfg.GeoLiteLicenseKey != "") {
		geoWarning = h.E("div", []h.Attr{h.A("class", "alert warning")},
			h.Text("Geolocation is off: set GORT_GEOLITE_LICENSE_KEY to enrich visits with country and city data."))
	}

	var recentRows []h.Node
	for _, d := range recent.Items {
		recentRows = append(recentRows, h.E("tr", nil,
			h.E("td", nil,
				h.E("a", []h.Attr{h.A("class", "mono"), h.A("href", fmt.Sprintf("/admin/short-urls/%d/edit", d.Id))},
					h.Text(d.Authority+"/"+d.ShortCode))),
			h.E("td", nil,
				h.E("span", []h.Attr{h.A("class", "truncate")}, h.Text(d.LongUrl))),
			h.E("td", nil, h.Text(strconv.FormatInt(d.VisitCount, 10))),
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(formatDateTime(d.CreatedAt)))))
	}

	content := []h.Node{
		h.E("h1", nil, h.Text("Overview")),
		geoWarning,
		h.E("div", []h.Attr{h.A("class", "stat-grid")},
			statTile("Short URLs", strconv.FormatInt(o.ShortUrlCount, 10), "/admin/short-urls"),
			statTile("Visits", strconv.FormatInt(o.VisitCount, 10), ""),
			statTile("Orphan visits", strconv.FormatInt(o.OrphanVisitCount, 10), "/admin/visits/orphan"),
			statTile("Tags", strconv.FormatInt(o.TagCount, 10), "/admin/tags"),
			statTile("Bot visits", strconv.FormatInt(o.BotVisitCount, 10), "")),
		h.E("div", []h.Attr{h.A("class", "card chart-card"), h.A("style", "margin-top:1rem")},
			h.E("h2", []h.Attr{h.A("style", "margin-top:0")}, h.Text("Visits — last 30 days")),
			chartVisitsPerDay(series)),
		h.E("h2", nil, h.Text("Latest short URLs")),
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Short URL")),
						h.E("th", nil, h.Text("Long URL")),
						h.E("th", nil, h.Text("Visits")),
						h.E("th", nil, h.Text("Created (UTC)")))),
				h.E("tbody", nil, recentRows...))),
	}
	respondPage(w, user, "/admin", "Overview", content)
}
