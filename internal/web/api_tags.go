package web

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type RenameTagBody struct {
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
}

// GET /rest/v1/tags?withStats=true&searchTerm=&page=&itemsPerPage=
func (a *App) opListTags(ctx context.Context, key *AuthenticatedKey, in *TagListInput) (*PageDTO[any], error) {
	q := queryValues(in)
	withStats := queryBool(q, "withStats")
	page := queryIntDefault(q, "page", 1)
	itemsPerPage := queryIntDefault(q, "itemsPerPage", core.MaxPageSize)

	pageResult, err := data.ListTags(ctx, a.DB, q.Get("searchTerm"), page, itemsPerPage)
	if err != nil {
		return nil, err
	}

	if withStats {
		if key.Role.Kind != core.RoleAdmin {
			return nil, Forbidden("Only admin keys can view tag statistics.")
		}
		return result(NewPageDTO(pageResult, func(t data.TagStatsRow) any {
			return tagStatsDTO{Tag: t.Name, ShortURLsCount: t.ShortURLCount, VisitsCount: t.VisitCount}
		}))
	}
	return result(NewPageDTO(pageResult, func(t data.TagStatsRow) any { return t.Name }))
}

// PUT /rest/v1/tags — rename
func (a *App) opRenameTag(ctx context.Context, key *AuthenticatedKey, in *BodyInput[RenameTagBody]) (*RenameTagBody, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can change global tags.")
	}
	body := &in.Body
	newName, err := core.NewTagName(body.NewName)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	if err := data.RenameTag(ctx, a.DB, body.OldName, newName); err != nil {
		var renameErr *data.TagRenameError
		if errors.As(err, &renameErr) {
			if renameErr.NameTaken {
				return nil, Conflict("tag-conflict", fmt.Sprintf("A tag named '%s' already exists.", renameErr.Name))
			}
			return nil, NotFound(fmt.Sprintf("Tag '%s' was not found.", renameErr.Name))
		}
		return nil, err
	}
	return result(RenameTagBody{OldName: body.OldName, NewName: newName.Value()})
}

// DELETE /rest/v1/tags?tags[]=a&tags[]=b (also accepts tags=a,b)
func (a *App) opDeleteTags(ctx context.Context, key *AuthenticatedKey, in *DeleteTagsInput) (*DeletedTags, error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can change global tags.")
	}
	var tags []string
	for _, listed := range queryStringList(queryValues(in), "tags") {
		for _, tag := range strings.Split(listed, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	if len(tags) == 0 {
		return nil, BadRequest("Provide at least one tag to delete via ?tags[]=.")
	}
	deleted, err := data.DeleteTags(ctx, a.DB, tags)
	if err != nil {
		return nil, err
	}
	return result(DeletedTags{DeletedTags: deleted})
}

// GET /rest/v1/tags/{tag}/visits
func (a *App) opTagVisits(ctx context.Context, key *AuthenticatedKey, in *TagVisitsInput) (*PageDTO[VisitDTO], error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can view tag visits.")
	}
	tag := in.Tag
	exists, err := data.TagExists(ctx, a.DB, tag)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFound(fmt.Sprintf("Tag '%s' was not found.", tag))
	}
	page, err := data.ListVisitsForTag(ctx, a.DB, tag, visitFiltersFromQuery(queryValues(in)))
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, NewVisitDTO))
}

type tagStatsDTO struct {
	Tag            string `json:"tag"`
	ShortURLsCount int64  `json:"shortUrlsCount"`
	VisitsCount    int64  `json:"visitsCount"`
}

func (a *App) opTagStats(ctx context.Context, key *AuthenticatedKey, in *TagListInput) (*PageDTO[tagStatsDTO], error) {
	if key.Role.Kind != core.RoleAdmin {
		return nil, Forbidden("Only admin keys can view tag statistics.")
	}
	q := queryValues(in)
	page, err := data.ListTags(ctx, a.DB, q.Get("searchTerm"), queryIntDefault(q, "page", 1), queryIntDefault(q, "itemsPerPage", core.MaxPageSize))
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, func(t data.TagStatsRow) tagStatsDTO {
		return tagStatsDTO{Tag: t.Name, ShortURLsCount: t.ShortURLCount, VisitsCount: t.VisitCount}
	}))
}
