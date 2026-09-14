package web

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/olmesm/gort/internal/data"
)

const listPageSize = 25

func listFilters(q url.Values) data.ListFilters {
	return data.ListFilters{Search: strings.TrimSpace(q.Get("search")), Page: queryIntDefault(q, "page", 1), ItemsPerPage: listPageSize}
}

// listPageURL keeps active filters when moving between pages.
func listPageURL(base string, q url.Values, page int) string {
	values := url.Values{}
	for key, items := range q {
		if key != "page" {
			for _, value := range items {
				if value != "" {
					values.Add(key, value)
				}
			}
		}
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if len(values) == 0 {
		return base
	}
	return base + "?" + values.Encode()
}

type listControlsView struct {
	BasePath    string
	Search      string
	Placeholder string
	Selects     []listSelectView
}

type listSelectView struct {
	Name    string
	Label   string
	Options []optionView
}

func listSelect(q url.Values, name, label string, values ...string) listSelectView {
	return listSelectView{Name: name, Label: label, Options: optionsOf(values, q.Get(name))}
}

func listControls(base string, q url.Values, placeholder string, selects ...listSelectView) listControlsView {
	return listControlsView{BasePath: base, Search: q.Get("search"), Placeholder: placeholder, Selects: selects}
}
