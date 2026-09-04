package web

import (
	"fmt"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/data"
)

// Server-rendered SVG charts (no client-side JS needed).

func inv(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// chartVisitsPerDay renders daily visit counts as a filled line chart.
// Fills gaps between days with zeroes.
func chartVisitsPerDay(series []data.DayCount) template.HTML {
	const width, height = 720.0, 200.0
	const padL, padR, padT, padB = 40.0, 10.0, 10.0, 22.0

	if len(series) == 0 {
		return `<div class="muted">No visits recorded in this period yet.</div>`
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
	return template.HTML(sb.String())
}

// barRowView is one bar in the "bar-list" template partial.
type barRowView struct {
	Label string
	Pct   template.CSS
	Count int64
}

// barRows scales counts against the max value for the bar-list partial.
func barRows(rows []data.LabelCount) []barRowView {
	maxV := int64(1)
	for _, row := range rows {
		if row.Count > maxV {
			maxV = row.Count
		}
	}
	out := make([]barRowView, len(rows))
	for i, row := range rows {
		label := "Unknown"
		if row.Label != nil {
			label = *row.Label
		}
		out[i] = barRowView{
			Label: label,
			Pct:   template.CSS(inv(float64(row.Count) / float64(maxV) * 100.0)),
			Count: row.Count,
		}
	}
	return out
}
