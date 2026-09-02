package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
)

func tagTable(page core.Page[data.TagStatsRow]) h.Node {
	var rows []h.Node
	for _, t := range page.Items {
		rows = append(rows, h.E("tr", nil,
			h.E("td", nil, h.E("span", []h.Attr{h.A("class", "badge")}, h.Text(t.Name))),
			h.E("td", nil,
				h.E("a", []h.Attr{h.A("href", "/admin/short-urls?tag="+url.QueryEscape(t.Name))},
					h.Text(strconv.FormatInt(t.ShortUrlCount, 10)))),
			h.E("td", nil, h.Text(strconv.FormatInt(t.VisitCount, 10))),
			h.E("td", nil,
				h.E("form", []h.Attr{h.A("class", "inline"), h.A("method", "post"), h.A("action", "/admin/tags/rename")},
					h.E("input", []h.Attr{h.A("type", "hidden"), h.A("name", "oldName"), h.A("value", t.Name)}),
					h.E("input", []h.Attr{
						h.A("type", "text"), h.A("name", "newName"), h.A("value", t.Name),
						h.A("style", "width:10rem"),
					}),
					h.Text(" "),
					h.E("button", []h.Attr{h.A("class", "secondary small")}, h.Text("Rename")))),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.E("form", []h.Attr{
					h.A("class", "inline"), h.A("method", "post"), h.A("action", "/admin/tags/delete"),
					h.A("onsubmit", "return confirm('Delete this tag? Short URLs keep working.')"),
				},
					h.E("input", []h.Attr{h.A("type", "hidden"), h.A("name", "name"), h.A("value", t.Name)}),
					h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Delete"))))))
	}

	return h.E("div", []h.Attr{h.A("id", "tag-table")},
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("Tag")),
						h.E("th", nil, h.Text("Short URLs")),
						h.E("th", nil, h.Text("Visits")),
						h.E("th", nil, h.Text("Rename")),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		pager(func(p int) string {
			if p == 1 {
				return "/admin/tags"
			}
			return fmt.Sprintf("/admin/tags?page=%d", p)
		}, page))
}

// GET /admin/tags
func (a *App) uiListTags(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("search")
	page := queryIntDefault(q, "page", 1)
	result, err := data.ListTags(a.Db, search, page, 25)
	if err != nil {
		a.serverError(w, err)
		return
	}
	table := tagTable(result)

	if isHtmx(r) {
		respondFragment(w, table)
		return
	}

	content := []h.Node{
		h.E("h1", nil, h.Text("Tags")),
		h.E("div", []h.Attr{h.A("class", "toolbar")},
			h.E("form", []h.Attr{
				hxGet("/admin/tags"),
				hxTarget("#tag-table"),
				hxSwap("outerHTML"),
				hxTrigger("submit, input delay:400ms from:input[name='search']"),
				h.A("method", "get"),
				h.A("action", "/admin/tags"),
			},
				h.E("input", []h.Attr{
					h.A("type", "search"), h.A("name", "search"), h.A("value", search),
					h.A("placeholder", "Search tags…"),
				}),
				h.E("button", []h.Attr{h.A("class", "secondary")}, h.Text("Search")))),
		table,
	}
	respondPage(w, user, "/admin/tags", "Tags", content)
}

// POST /admin/tags/rename
func (a *App) uiRenameTag(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	oldName := r.PostFormValue("oldName")

	var message string
	newName, err := core.NewTagName(r.PostFormValue("newName"))
	if err != nil {
		message = err.Error()
	} else if err := data.RenameTag(a.Db, oldName, newName); err != nil {
		var renameErr *data.TagRenameError
		if errors.As(err, &renameErr) {
			message = renameErr.Error()
		} else {
			a.serverError(w, err)
			return
		}
	}

	if message == "" {
		http.Redirect(w, r, "/admin/tags", http.StatusFound)
		return
	}

	result, err := data.ListTags(a.Db, "", 1, 25)
	if err != nil {
		a.serverError(w, err)
		return
	}
	content := []h.Node{
		h.E("h1", nil, h.Text("Tags")),
		alertError(message),
		tagTable(result),
	}
	respondHtml(w, http.StatusBadRequest, layoutPage(user, "/admin/tags", "Tags", content))
}

// POST /admin/tags/delete
func (a *App) uiDeleteTag(_ *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err == nil {
		if name := r.PostFormValue("name"); name != "" {
			if _, err := data.DeleteTags(a.Db, []string{name}); err != nil {
				a.serverError(w, err)
				return
			}
		}
	}
	http.Redirect(w, r, "/admin/tags", http.StatusFound)
}
