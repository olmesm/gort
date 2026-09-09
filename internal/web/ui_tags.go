package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type tagTableView struct {
	Tags  []data.TagStatsRow
	Pager pagerView
}

func tagTable(page core.Page[data.TagStatsRow]) tagTableView {
	return tagTableView{
		Tags: page.Items,
		Pager: newPager(page, func(p int) string {
			if p == 1 {
				return "/admin/tags"
			}
			return fmt.Sprintf("/admin/tags?page=%d", p)
		}),
	}
}

type tagsView struct {
	Error  string
	Search string
	Table  tagTableView
}

// GET /admin/tags
func (a *App) uiListTags(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	search := q.Get("search")
	result, err := data.ListTags(r.Context(), a.DB, search, queryIntDefault(q, "page", 1), 25)
	if err != nil {
		return err
	}
	table := tagTable(result)

	if isHtmx(r) {
		return a.renderShared(w, http.StatusOK, "tag-table", table)
	}
	a.renderPage(w, http.StatusOK, "tags", user, "/admin/tags", "Tags", tagsView{
		Search: search,
		Table:  table,
	})
	return nil
}

// POST /admin/tags/rename
func (a *App) uiRenameTag(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	oldName := r.PostFormValue("oldName")

	var message string
	newName, err := core.NewTagName(r.PostFormValue("newName"))
	if err != nil {
		message = err.Error()
	} else if err := data.RenameTag(r.Context(), a.DB, oldName, newName); err != nil {
		var renameErr *data.TagRenameError
		if errors.As(err, &renameErr) {
			message = renameErr.Error()
		} else {
			return err
		}
	}

	if message == "" {
		return redirect(w, r, "/admin/tags")
	}

	result, err := data.ListTags(r.Context(), a.DB, "", 1, 25)
	if err != nil {
		return err
	}
	a.renderPage(w, http.StatusBadRequest, "tags", user, "/admin/tags", "Tags", tagsView{
		Error: message,
		Table: tagTable(result),
	})
	return nil
}

// POST /admin/tags/delete
func (a *App) uiDeleteTag(_ *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if name := r.PostFormValue("name"); name != "" {
		if _, err := data.DeleteTags(r.Context(), a.DB, []string{name}); err != nil {
			return err
		}
	}
	return redirect(w, r, "/admin/tags")
}
