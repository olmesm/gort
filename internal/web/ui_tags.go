package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type tagTableView struct {
	Rows  []tagRowView
	Pager pagerView
}

type tagRowView struct {
	Name          string
	FilterUrl     string
	ShortUrlCount int64
	VisitCount    int64
}

func tagTable(page core.Page[data.TagStatsRow]) tagTableView {
	table := tagTableView{
		Pager: newPager(page, func(p int) string {
			if p == 1 {
				return "/admin/tags"
			}
			return fmt.Sprintf("/admin/tags?page=%d", p)
		}),
	}
	for _, t := range page.Items {
		table.Rows = append(table.Rows, tagRowView{
			Name:          t.Name,
			FilterUrl:     "/admin/short-urls?tag=" + url.QueryEscape(t.Name),
			ShortUrlCount: t.ShortUrlCount,
			VisitCount:    t.VisitCount,
		})
	}
	return table
}

type tagsView struct {
	Error  string
	Search string
	Table  tagTableView
}

// GET /admin/tags
func (a *App) uiListTags(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("search")
	result, err := data.ListTags(a.Db, search, queryIntDefault(q, "page", 1), 25)
	if err != nil {
		a.serverError(w, err)
		return
	}
	table := tagTable(result)

	if isHtmx(r) {
		a.renderShared(w, http.StatusOK, "tag-table", table)
		return
	}
	a.renderPage(w, http.StatusOK, "tags", user, "/admin/tags", "Tags", tagsView{
		Search: search,
		Table:  table,
	})
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
	a.renderPage(w, http.StatusBadRequest, "tags", user, "/admin/tags", "Tags", tagsView{
		Error: message,
		Table: tagTable(result),
	})
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
