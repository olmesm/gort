package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/data"
)

// Server-rendered SVG charts (no client-side JS needed).

func inv(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

type chartLabelView struct{ X, Y, Text string }
type chartPointView struct {
	X, Y, Date string
	Count      int64
}
type visitChartView struct {
	Grid               []chartLabelView
	Points             []chartPointView
	Labels             []chartLabelView
	LinePath, AreaPath string
}

// chartVisitsPerDay renders daily visit counts as a filled line chart.
// Fills gaps between days with zeroes.
func chartVisitsPerDay(series []data.DayCount) visitChartView {
	const width, height = 1200.0, 200.0
	const padL, padR, padT, padB = 40.0, 40.0, 10.0, 22.0

	if len(series) == 0 {
		return visitChartView{}
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

	chart := visitChartView{}
	for _, gy := range []float64{0, 0.5, 1} {
		chart.Grid = append(chart.Grid, chartLabelView{Y: inv(padT + plotH*gy), Text: strconv.FormatInt(int64(float64(maxY)*(1-gy)), 10)})
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
	chart.LinePath, chart.AreaPath = linePath, areaPath
	labelEvery := max(1, n/8)
	for i, p := range points {
		chart.Points = append(chart.Points, chartPointView{X: inv(x(i)), Y: inv(y(p.count)), Date: p.day.Format("2006-01-02"), Count: p.count})
		if i%labelEvery == 0 || i == n-1 {
			chart.Labels = append(chart.Labels, chartLabelView{X: inv(x(i)), Y: inv(height - 6), Text: p.day.Format("01-02")})
		}
	}
	return chart
}

// barRowView is one bar in the "bar-list" template partial.
type barRowView struct {
	Label string
	Pct   float64
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
			Pct:   float64(row.Count) / float64(maxV) * 100.0,
			Count: row.Count,
		}
	}
	return out
}
