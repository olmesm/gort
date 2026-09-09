package web

import (
	"errors"
	"strings"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// Application services: orchestration between the domain core and the
// repositories. All validation lives in internal/core — these functions only
// sequence IO around already-validated values.

// ResolveRequestDomain resolves the domain row for an incoming request Host
// (falling back to the default domain).
func (a *App) ResolveRequestDomain(hostAuthority string) (*data.DomainRow, error) {
	byHost, err := data.DomainByAuthority(a.Db, strings.ToLower(hostAuthority))
	if err != nil {
		return nil, err
	}
	if byHost != nil {
		return byHost, nil
	}
	return data.DefaultDomain(a.Db)
}

// ResolveNamedDomain resolves an explicitly named domain (API "domain"
// param). Empty → default domain; unknown → nil.
func (a *App) ResolveNamedDomain(authority string) (*data.DomainRow, error) {
	if authority == "" {
		return data.DefaultDomain(a.Db)
	}
	return data.DomainByAuthority(a.Db, strings.ToLower(strings.TrimSpace(authority)))
}

// LifetimeOfRow is the lifetime stored on a row. Values were validated on
// the way in, so this is a plain projection.
func LifetimeOfRow(row *data.ShortUrlRow) core.Lifetime {
	return core.Lifetime{
		ValidSince: row.ValidSince,
		ValidUntil: row.ValidUntil,
		MaxVisits:  row.MaxVisits,
	}
}

// resolveTargetDomain resolves the spec's target domain, auto-registering
// unknown authorities.
func (a *App) resolveTargetDomain(domain *core.DomainAuthority) (*data.DomainRow, error) {
	if domain == nil {
		return data.DefaultDomain(a.Db)
	}
	existing, err := data.DomainByAuthority(a.Db, domain.Value())
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	created, err := data.CreateDomain(a.Db, *domain)
	if err != nil {
		return nil, err
	}
	if created != nil {
		return created, nil
	}
	// Lost a race with a concurrent insert; fetch the winner.
	return data.DomainByAuthority(a.Db, domain.Value())
}

// Author says who created a short URL: a dashboard user or an API key.
type Author struct {
	UserId   *core.UserID
	ApiKeyId *core.ApiKeyID
}

func UserAuthor(id core.UserID) *Author     { return &Author{UserId: &id} }
func ApiKeyAuthor(id core.ApiKeyID) *Author { return &Author{ApiKeyId: &id} }

// insertWithCode inserts with the spec's slug, or retries generated codes
// until one is free.
func (a *App) insertWithCode(spec *core.ShortUrlSpec, domain *data.DomainRow, record func(core.ShortCode) data.NewShortUrl) (core.ShortUrlID, error) {
	if spec.CustomSlug != nil {
		id, err := data.CreateShortUrl(a.Db, record(*spec.CustomSlug), spec.Tags)
		if errors.Is(err, data.ErrDuplicateShortCode) {
			return 0, core.SlugInUseError(spec.CustomSlug.Value(), domain.Authority)
		}
		if err != nil {
			return 0, err
		}
		return id, nil
	}

	codeLength := a.Cfg.ShortCodeLength
	if spec.CodeLength != nil {
		codeLength = *spec.CodeLength
	}
	if codeLength < core.MinCodeLength {
		codeLength = core.MinCodeLength
	}

	for attempt := 0; attempt < 10; attempt++ {
		id, err := data.CreateShortUrl(a.Db, record(core.GenerateShortCode(codeLength)), spec.Tags)
		if errors.Is(err, data.ErrDuplicateShortCode) {
			continue
		}
		if err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, core.CodeGenerationExhaustedError()
}

// CreateShortUrl creates a short URL from a validated spec: domain resolution
// (auto-registering unknown domains), code generation with collision retry,
// atomic insert with tags, async title resolution and event publication.
func (a *App) CreateShortUrl(author *Author, spec *core.ShortUrlSpec) (*ShortUrlDto, error) {
	domain, err := a.resolveTargetDomain(spec.Domain)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, core.NewError(core.ErrUnknownDomain, "The domain is not registered.")
	}

	if spec.FindIfExists {
		existing, err := data.ShortUrlDetailByLongUrl(a.Db, core.DomainID(domain.Id), spec.LongUrl)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			tags, err := data.TagsForShortUrl(a.Db, core.ShortUrlID(existing.Id))
			if err != nil {
				return nil, err
			}
			dto := NewShortUrlDto(a.Cfg, tags, existing)
			return &dto, nil
		}
	}

	record := func(code core.ShortCode) data.NewShortUrl {
		status := a.Cfg.DefaultRedirectStatus
		if spec.RedirectStatus != nil {
			status = *spec.RedirectStatus
		}
		forwardQuery := true
		if spec.ForwardQuery != nil {
			forwardQuery = *spec.ForwardQuery
		}
		crawlable := false
		if spec.Crawlable != nil {
			crawlable = *spec.Crawlable
		}
		nu := data.NewShortUrl{
			ShortCode:      code,
			DomainId:       core.DomainID(domain.Id),
			LongUrl:        spec.LongUrl,
			Title:          spec.Title,
			RedirectStatus: status,
			ForwardQuery:   forwardQuery,
			Crawlable:      crawlable,
			Lifetime:       spec.Lifetime,
		}
		if spec.Group != nil {
			group := spec.Group.Value()
			nu.GroupName = &group
		}
		if author != nil {
			nu.AuthorUserId = author.UserId
			nu.AuthorApiKeyId = author.ApiKeyId
		}
		return nu
	}

	id, err := a.insertWithCode(spec, domain, record)
	if err != nil {
		return nil, err
	}

	if a.Cfg.AutoResolveTitles && spec.Title == nil {
		a.Queues.enqueueTitle(id, spec.LongUrl)
	}

	detail, err := data.ShortUrlDetailByID(a.Db, id)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, errors.New("short URL missing immediately after insert")
	}

	dto := NewShortUrlDto(a.Cfg, core.TagValues(spec.Tags), detail)
	a.Queues.PublishEvent(UrlCreatedEvent(dto))
	return &dto, nil
}

// EditShortUrl applies a validated edit to an existing short URL.
func (a *App) EditShortUrl(id core.ShortUrlID, current *data.ShortUrlDetail, edit *core.ShortUrlEdit) (*ShortUrlDto, error) {
	titleUnchanged := (edit.Title == nil && current.Title == nil) ||
		(edit.Title != nil && current.Title != nil && *edit.Title == *current.Title)

	update := data.ShortUrlUpdate{
		LongUrl:              edit.LongUrl,
		Title:                edit.Title,
		TitleWasAutoResolved: titleUnchanged && current.TitleWasAutoResolved,
		RedirectStatus:       edit.RedirectStatus,
		ForwardQuery:         edit.ForwardQuery,
		Crawlable:            edit.Crawlable,
		Lifetime:             edit.Lifetime,
	}
	if edit.Group != nil {
		group := edit.Group.Value()
		update.GroupName = &group
	}

	if _, err := data.UpdateShortUrl(a.Db, id, update); err != nil {
		return nil, err
	}
	if edit.ChangeTags {
		if err := data.SetShortUrlTags(a.Db, id, edit.Tags); err != nil {
			return nil, err
		}
	}

	updated, err := data.ShortUrlDetailByID(a.Db, id)
	if err != nil {
		return nil, err
	}
	tagNames, err := data.TagsForShortUrl(a.Db, id)
	if err != nil {
		return nil, err
	}
	// The row was just updated under this id; absence would be a bug.
	dto := NewShortUrlDto(a.Cfg, tagNames, updated)
	return &dto, nil
}
