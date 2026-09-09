package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type RenameTagBody struct {
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
}

// GET /rest/v1/tags?withStats=true&searchTerm=&page=&itemsPerPage=
func (a *App) apiListTags(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	withStats := queryBool(q, "withStats")
	page := queryIntDefault(q, "page", 1)
	itemsPerPage := queryIntDefault(q, "itemsPerPage", core.MaxPageSize)

	result, err := data.ListTags(r.Context(), a.Db, q.Get("searchTerm"), page, itemsPerPage)
	if err != nil {
		return err
	}

	if withStats {
		type tagStatsDto struct {
			Tag            string `json:"tag"`
			ShortUrlsCount int64  `json:"shortUrlsCount"`
			VisitsCount    int64  `json:"visitsCount"`
		}
		RespondJSON(w, http.StatusOK, NewPageDto(result, func(t data.TagStatsRow) tagStatsDto {
			return tagStatsDto{Tag: t.Name, ShortUrlsCount: t.ShortUrlCount, VisitsCount: t.VisitCount}
		}))
		return nil
	}
	return RespondJSON(w, http.StatusOK, NewPageDto(result, func(t data.TagStatsRow) string { return t.Name }))
}

// PUT /rest/v1/tags — rename
func (a *App) apiRenameTag(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[RenameTagBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	newName, err := core.NewTagName(body.NewName)
	if err != nil {
		return BadRequest(err.Error())
	}
	if err := data.RenameTag(r.Context(), a.Db, body.OldName, newName); err != nil {
		var renameErr *data.TagRenameError
		if errors.As(err, &renameErr) {
			if renameErr.NameTaken {
				return Conflict("tag-conflict", fmt.Sprintf("A tag named '%s' already exists.", renameErr.Name))
			}
			return NotFound(fmt.Sprintf("Tag '%s' was not found.", renameErr.Name))
		}
		return err
	}
	return RespondJSON(w, http.StatusOK, map[string]string{"oldName": body.OldName, "newName": newName.Value()})
}

// DELETE /rest/v1/tags?tags[]=a&tags[]=b (also accepts tags=a,b)
func (a *App) apiDeleteTags(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	var tags []string
	for _, listed := range queryStringList(r.URL.Query(), "tags") {
		for _, tag := range strings.Split(listed, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	if len(tags) == 0 {
		return BadRequest("Provide at least one tag to delete via ?tags[]=.")
	}
	deleted, err := data.DeleteTags(r.Context(), a.Db, tags)
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, map[string]int{"deletedTags": deleted})
}

// GET /rest/v1/tags/{tag}/visits
func (a *App) apiTagVisits(_ *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	tag := r.PathValue("tag")
	exists, err := data.TagExists(r.Context(), a.Db, tag)
	if err != nil {
		return err
	}
	if !exists {
		return NotFound(fmt.Sprintf("Tag '%s' was not found.", tag))
	}
	page, err := data.ListVisitsForTag(r.Context(), a.Db, tag, visitFiltersFromQuery(r.URL.Query()))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}
