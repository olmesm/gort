package web

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/data"
	gh "github.com/olmesm/gort/internal/h"
)

// Server-rendered SVG charts (no client-side JS needed).

func inv(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// chartVisitsPerDay renders daily visit counts as a filled line chart.
// Fills gaps between days with zeroes.
func chartVisitsPerDay(series []data.DayCount) gh.Node {
	const width, height = 720.0, 200.0
	const padL, padR, padT, padB = 40.0, 10.0, 10.0, 22.0

	if len(series) == 0 {
		return gh.E("div", []gh.Attr{gh.A("class", "muted")}, gh.Text("No visits recorded in this period yet."))
	}

	// Expand to a contiguous day range so gaps show as zero.
	parse := func(s string) time.Time {
		t, _ := time.Parse("2006-01-02", s)
		return t
	}
	byDay := map[time.Time]int64{}
	minDay, maxDay := parse(series[0].Day), parse(series[0].Day)
	for _, dc := range series {
		day := parse(dc.Day)
		byDay[day] = dc.Count
		if day.Before(minDay) {
			minDay = day
		}
		if day.After(maxDay) {
			maxDay = day
		}
	}
	if maxDay.Sub(minDay) < 6*24*time.Hour {
		maxDay = minDay.AddDate(0, 0, 6)
	}
	type point struct {
		day   time.Time
		count int64
	}
	var points []point
	for day := minDay; !day.After(maxDay); day = day.AddDate(0, 0, 1) {
		points = append(points, point{day, byDay[day]})
	}

	maxY := int64(1)
	for _, p := range points {
		if p.count > maxY {
			maxY = p.count
		}
	}
	n := len(points)
	plotW := width - padL - padR
	plotH := height - padT - padB
	x := func(i int) float64 {
		if n == 1 {
			return padL + plotW/2
		}
		return padL + plotW*float64(i)/float64(n-1)
	}
	y := func(v int64) float64 {
		return padT + plotH - plotH*float64(v)/float64(maxY)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb,
		`<svg viewBox="0 0 %s %s" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Visits per day">`,
		inv(width), inv(height))

	// Horizontal gridlines + y labels
	for _, gy := range []float64{0.0, 0.5, 1.0} {
		value := float64(maxY) * (1.0 - gy)
		yy := padT + plotH*gy
		fmt.Fprintf(&sb,
			`<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#222738" stroke-width="1"/>`,
			inv(padL), inv(yy), inv(width-padR), inv(yy))
		fmt.Fprintf(&sb,
			`<text x="%s" y="%s" font-size="11" fill="#6b7385" text-anchor="end">%d</text>`,
			inv(padL-6.0), inv(yy+4.0), int64(value))
	}

	// Area + line
	var lineParts []string
	for i, p := range points {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		lineParts = append(lineParts, fmt.Sprintf("%s%s,%s", cmd, inv(x(i)), inv(y(p.count))))
	}
	linePath := strings.Join(lineParts, " ")
	areaPath := linePath + fmt.Sprintf(" L%s,%s L%s,%s Z",
		inv(x(n-1)), inv(padT+plotH), inv(x(0)), inv(padT+plotH))
	fmt.Fprintf(&sb, `<path d="%s" fill="rgba(129,140,248,0.16)" stroke="none"/>`, areaPath)
	fmt.Fprintf(&sb, `<path d="%s" fill="none" stroke="#818cf8" stroke-width="2"/>`, linePath)

	// Dots with tooltips
	for i, p := range points {
		label := html.EscapeString(p.day.Format("2006-01-02"))
		fmt.Fprintf(&sb,
			`<circle cx="%s" cy="%s" r="2.5" fill="#818cf8"><title>%s: %d</title></circle>`,
			inv(x(i)), inv(y(p.count)), label, p.count)
	}

	// Sparse x labels
	labelEvery := n / 8
	if labelEvery < 1 {
		labelEvery = 1
	}
	for i, p := range points {
		if i%labelEvery == 0 || i == n-1 {
			label := html.EscapeString(p.day.Format("01-02"))
			fmt.Fprintf(&sb,
				`<text x="%s" y="%s" font-size="10" fill="#6b7385" text-anchor="middle">%s</text>`,
				inv(x(i)), inv(height-6.0), label)
		}
	}

	sb.WriteString("</svg>")
	return gh.Raw(sb.String())
}

// chartBarList renders a horizontal bar list (label + count), scaled to the
// max value.
func chartBarList(rows []data.LabelCount) gh.Node {
	if len(rows) == 0 {
		return gh.E("div", []gh.Attr{gh.A("class", "muted")}, gh.Text("No data yet."))
	}
	maxV := int64(1)
	for _, row := range rows {
		if row.Count > maxV {
			maxV = row.Count
		}
	}
	var trs []gh.Node
	for _, row := range rows {
		label := "Unknown"
		if row.Label != nil {
			label = *row.Label
		}
		pct := float64(row.Count) / float64(maxV) * 100.0
		trs = append(trs, gh.E("tr", nil,
			gh.E("td", []gh.Attr{gh.A("style", "width:35%")}, gh.Text(label)),
			gh.E("td", nil,
				gh.E("div", []gh.Attr{gh.A("style", fmt.Sprintf(
					"background:rgba(129,140,248,0.35);border-radius:4px;height:1.1rem;width:%s%%;min-width:2px",
					inv(pct)))})),
			gh.E("td", []gh.Attr{gh.A("style", "width:4rem;text-align:right")},
				gh.Text(strconv.FormatInt(row.Count, 10)))))
	}
	return gh.E("table", nil, gh.E("tbody", nil, trs...))
}
