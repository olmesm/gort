package web

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateShortURLBody struct {
	LongURL         string     `json:"longUrl" example:"https://example.com/article"`
	CustomSlug      *string    `json:"customSlug" required:"false"`
	ShortCodeLength *int       `json:"shortCodeLength" required:"false"`
	Domain          *string    `json:"domain" required:"false"`
	Title           *string    `json:"title" required:"false" doc:"Optional title. Omit to resolve it from the destination when enabled."`
	Tags            []string   `json:"tags" required:"false"`
	Group           *string    `json:"group" required:"false"`
	MaxVisits       *int64     `json:"maxVisits" required:"false"`
	ValidSince      *time.Time `json:"validSince" required:"false"`
	ValidUntil      *time.Time `json:"validUntil" required:"false"`
	ForwardQuery    *bool      `json:"forwardQuery" required:"false"`
	Crawlable       *bool      `json:"crawlable" required:"false"`
	RedirectStatus  *int       `json:"redirectStatus" required:"false"`
	FindIfExists    *bool      `json:"findIfExists" required:"false"`
}

// EditShortUrlBody is the PATCH body: absent = leave unchanged; explicit null
// clears optional fields.
type EditShortURLBody struct {
	LongURL        Field[string]    `json:"longUrl" required:"false"`
	Title          Field[string]    `json:"title" required:"false"`
	Tags           Field[[]string]  `json:"tags" required:"false"`
	Group          Field[string]    `json:"group" required:"false"`
	MaxVisits      Field[int64]     `json:"maxVisits" required:"false"`
	ValidSince     Field[time.Time] `json:"validSince" required:"false"`
	ValidUntil     Field[time.Time] `json:"validUntil" required:"false"`
	ForwardQuery   Field[bool]      `json:"forwardQuery" required:"false"`
	Crawlable      Field[bool]      `json:"crawlable" required:"false"`
	RedirectStatus Field[int]       `json:"redirectStatus" required:"false"`
}

type RuleConditionBody struct {
	Type       string  `json:"type"`
	MatchKey   *string `json:"matchKey,omitempty"`
	MatchValue string  `json:"matchValue"`
}

type RuleBody struct {
	LongURL    string              `json:"longUrl" example:"https://example.com/article"`
	Conditions []RuleConditionBody `json:"conditions"`
}

type SetRulesBody struct {
	RedirectRules []RuleBody `json:"redirectRules" required:"false"`
}

func conditionToBody(c core.RuleCondition) RuleConditionBody {
	body := RuleConditionBody{Type: string(c.Type), MatchValue: c.Value}
	if c.Type == core.CondQueryParam {
		key := c.Key
		body.MatchKey = &key
	}
	return body
}

func parseConditionBody(c RuleConditionBody) (core.RuleCondition, error) {
	switch c.Type {
	case "device":
		d, ok := core.DeviceOfSlug(c.MatchValue)
		if !ok {
			return core.RuleCondition{}, fmt.Errorf("Unknown device '%s'. Use android, ios, mobile or desktop.", c.MatchValue)
		}
		return core.DeviceIs(d), nil
	case "language":
		if strings.TrimSpace(c.MatchValue) == "" {
			return core.RuleCondition{}, fmt.Errorf("Language conditions need a matchValue.")
		}
		return core.LanguageIs(strings.TrimSpace(c.MatchValue)), nil
	case "query-param":
		if c.MatchKey == nil || strings.TrimSpace(*c.MatchKey) == "" {
			return core.RuleCondition{}, fmt.Errorf("Query-param conditions need a matchKey.")
		}
		return core.QueryParamIs(strings.TrimSpace(*c.MatchKey), c.MatchValue), nil
	case "ip-address":
		if strings.TrimSpace(c.MatchValue) == "" {
			return core.RuleCondition{}, fmt.Errorf("IP conditions need a matchValue (address or CIDR).")
		}
		return core.IPInRange(strings.TrimSpace(c.MatchValue)), nil
	default:
		return core.RuleCondition{}, fmt.Errorf("Unknown condition type '%s'. Use device, language, query-param or ip-address.", c.Type)
	}
}

func parseRule(index int, rule RuleBody) (core.RedirectRule, error) {
	longURL, err := core.NewLongURL(rule.LongURL)
	if err != nil {
		return core.RedirectRule{}, fmt.Errorf("Rule %d: %s", index+1, err)
	}
	var conditions []core.RuleCondition
	for _, c := range rule.Conditions {
		cond, err := parseConditionBody(c)
		if err != nil {
			return core.RedirectRule{}, fmt.Errorf("Rule %d: %s", index+1, err)
		}
		conditions = append(conditions, cond)
	}
	if len(conditions) == 0 {
		return core.RedirectRule{}, fmt.Errorf("Rule %d needs at least one condition.", index+1)
	}
	return core.RedirectRule{Priority: index + 1, LongURL: longURL.Value(), Conditions: conditions}, nil
}

// isShortUrlUserError reports whether the error is one of the domain's
// user-facing categories (bad input), as opposed to an infrastructure
// failure.
func isShortURLUserError(err error) bool {
	for _, category := range []error{
		core.ErrInvalidLongURL, core.ErrInvalidSlug, core.ErrInvalidTag,
		core.ErrInvalidGroup, core.ErrInvalidLifetime, core.ErrInvalidRedirectStatus,
		core.ErrSlugInUse, core.ErrUnknownDomain, core.ErrCodeGenerationExhausted,
	} {
		if errors.Is(err, category) {
			return true
		}
	}
	return false
}

// shortUrlProblem maps a short-URL domain error to its API reply; errors
// outside the domain's categories pass through and become a logged 500.
func shortURLProblem(err error) error {
	switch {
	case errors.Is(err, core.ErrSlugInUse):
		return Conflict("non-unique-slug", err.Error())
	case errors.Is(err, core.ErrCodeGenerationExhausted):
		return NewProblem(500, "code-generation", "Could not generate a short code", err.Error())
	case isShortURLUserError(err):
		return BadRequest(err.Error())
	default:
		return err
	}
}

// GET /rest/v1/short-urls
func (a *App) opListShortURLs(ctx context.Context, key *AuthenticatedKey, in *shortURLListOptions) (*PageDTO[ShortURLDTO], error) {

	// An unknown ?domain= filter matches nothing (-1 is an impossible id).
	var domainFilter *core.DomainID
	if authority := in.Domain; authority != "" {
		d, err := data.DomainByAuthority(ctx, a.DB, strings.ToLower(authority))
		if err != nil {
			return nil, err
		}
		id := core.DomainID(-1)
		if d != nil {
			id = d.ID
		}
		domainFilter = &id
	}

	filters := in.ShortURLFilters
	filters.DomainID = domainFilter
	if filters.Group != nil {
		group := core.NormalizeGroup(*filters.Group)
		filters.Group = &group
	}
	filters = applyKeyScope(key, filters)

	page, err := data.ListShortURLs(ctx, a.DB, filters)
	if err != nil {
		return nil, err
	}
	ids := make([]core.ShortURLID, len(page.Items))
	for i, d := range page.Items {
		ids[i] = d.ID
	}
	tagsByURL, err := data.TagsForShortURLs(ctx, a.DB, ids)
	if err != nil {
		return nil, err
	}

	dto := NewPageDTO(page, func(d data.ShortURLDetail) ShortURLDTO {
		return NewShortURLDTO(a.Cfg, tagsByURL[d.ID], &d)
	})
	return result(dto)
}

// POST /rest/v1/short-urls
func (a *App) opCreateShortURL(ctx context.Context, key *AuthenticatedKey, in *BodyInput[CreateShortURLBody]) (*ShortURLDTO, error) {
	body := &in.Body

	// Domain-scoped keys may only create URLs on their own domain.
	if key.Role.Kind == core.RoleDomain {
		d, err := data.DomainByID(ctx, a.DB, key.Role.DomainID)
		if err != nil {
			return nil, err
		}
		allowed := d != nil && body.Domain != nil && strings.ToLower(*body.Domain) == d.Authority
		if !allowed {
			return nil, Forbidden("This API key may only create short URLs on its own domain.")
		}
	}

	findIfExists := body.FindIfExists != nil && *body.FindIfExists
	spec, err := core.NewShortURLSpec(core.ShortURLSpecInput{
		LongURL:        body.LongURL,
		CustomSlug:     body.CustomSlug,
		CodeLength:     body.ShortCodeLength,
		Domain:         body.Domain,
		Title:          body.Title,
		Tags:           body.Tags,
		Group:          body.Group,
		ValidSince:     body.ValidSince,
		ValidUntil:     body.ValidUntil,
		MaxVisits:      body.MaxVisits,
		RedirectStatus: body.RedirectStatus,
		ForwardQuery:   body.ForwardQuery,
		Crawlable:      body.Crawlable,
		FindIfExists:   findIfExists,
	})
	if err != nil {
		return nil, shortURLProblem(err)
	}

	dto, err := a.CreateShortURL(ctx, APIKeyAuthor(key), spec)
	if err != nil {
		return nil, shortURLProblem(err)
	}
	return dto, nil
}

// GET /rest/v1/short-urls/{code}
func (a *App) opGetShortURL(ctx context.Context, key *AuthenticatedKey, in *ShortURLInput) (*ShortURLDTO, error) {
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}
	tags, err := data.TagsForShortURL(ctx, a.DB, detail.ID)
	if err != nil {
		return nil, err
	}
	return result(NewShortURLDTO(a.Cfg, tags, detail))
}

// PATCH /rest/v1/short-urls/{code} — merge with current state, then validate
// the merged result as a whole through the edit spec.
func (a *App) opEditShortURL(ctx context.Context, key *AuthenticatedKey, in *ShortURLBodyInput[EditShortURLBody]) (*ShortURLDTO, error) {
	body := &in.Body
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}

	input := core.ShortURLEditInput{
		LongURL:        body.LongURL.PickValue(detail.LongURL),
		Title:          body.Title.Pick(detail.Title),
		Group:          body.Group.Pick(detail.GroupName),
		ValidSince:     body.ValidSince.Pick(detail.ValidSince),
		ValidUntil:     body.ValidUntil.Pick(detail.ValidUntil),
		MaxVisits:      body.MaxVisits.Pick(detail.MaxVisits),
		RedirectStatus: body.RedirectStatus.PickValue(detail.RedirectStatus),
		ForwardQuery:   body.ForwardQuery.PickValue(detail.ForwardQuery),
		Crawlable:      body.Crawlable.PickValue(detail.Crawlable),
	}
	if body.Tags.Present {
		input.ChangeTags = true
		input.Tags = body.Tags.Value
	}

	edit, err := core.NewShortURLEdit(input)
	if err != nil {
		return nil, shortURLProblem(err)
	}
	dto, err := a.EditShortURL(ctx, detail.ID, detail, edit)
	if err != nil {
		return nil, err
	}
	return dto, nil
}

// DELETE /rest/v1/short-urls/{code}
func (a *App) opDeleteShortURL(ctx context.Context, key *AuthenticatedKey, in *ShortURLInput) (*Empty, error) {
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}
	if _, err := data.DeleteShortURL(ctx, a.DB, detail.ID); err != nil {
		return nil, err
	}
	return nil, nil
}

type rulesDTO struct {
	DefaultLongURL string        `json:"defaultLongUrl"`
	RedirectRules  []ruleItemDTO `json:"redirectRules"`
}

type ruleItemDTO struct {
	LongURL    string              `json:"longUrl" example:"https://example.com/article"`
	Priority   int                 `json:"priority"`
	Conditions []RuleConditionBody `json:"conditions"`
}

func newRulesDTO(detail *data.ShortURLDetail, rules []core.RedirectRule) rulesDTO {
	items := make([]ruleItemDTO, len(rules))
	for i, rule := range rules {
		conditions := make([]RuleConditionBody, len(rule.Conditions))
		for j, c := range rule.Conditions {
			conditions[j] = conditionToBody(c)
		}
		items[i] = ruleItemDTO{LongURL: rule.LongURL, Priority: rule.Priority, Conditions: conditions}
	}
	return rulesDTO{DefaultLongURL: detail.LongURL, RedirectRules: items}
}

// GET /rest/v1/short-urls/{code}/redirect-rules
func (a *App) opGetRules(ctx context.Context, key *AuthenticatedKey, in *ShortURLInput) (*rulesDTO, error) {
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}
	rules, err := data.RedirectRules(ctx, a.DB, detail.ID)
	if err != nil {
		return nil, err
	}
	return result(newRulesDTO(detail, rules))
}

// POST /rest/v1/short-urls/{code}/redirect-rules — replaces all rules.
func (a *App) opSetRules(ctx context.Context, key *AuthenticatedKey, in *ShortURLBodyInput[SetRulesBody]) (*rulesDTO, error) {
	body := &in.Body
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}

	rules := make([]core.RedirectRule, len(body.RedirectRules))
	for i, rule := range body.RedirectRules {
		parsed, err := parseRule(i, rule)
		if err != nil {
			return nil, BadRequest(err.Error())
		}
		rules[i] = parsed
	}

	if err := data.SetRedirectRules(ctx, a.DB, detail.ID, rules); err != nil {
		return nil, err
	}
	return result(newRulesDTO(detail, rules))
}

// GET /rest/v1/short-urls/{code}/visits
func (a *App) opListShortURLVisits(ctx context.Context, key *AuthenticatedKey, in *shortURLVisitOptions) (*PageDTO[VisitDTO], error) {
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}
	page, err := data.ListVisitsForShortURL(ctx, a.DB, detail.ID, in.VisitFilters)
	if err != nil {
		return nil, err
	}
	return result(NewPageDTO(page, NewVisitDTO))
}

// DELETE /rest/v1/short-urls/{code}/visits
func (a *App) opDeleteShortURLVisits(ctx context.Context, key *AuthenticatedKey, in *ShortURLInput) (*DeletedVisits, error) {
	code := in.Code
	detail, err := a.findAccessibleShortURL(ctx, key, code, in.Domain)
	if err != nil {
		return nil, err
	}
	deleted, err := data.DeleteVisitsForShortURL(ctx, a.DB, detail.ID)
	if err != nil {
		return nil, err
	}
	return result(DeletedVisits{DeletedVisits: deleted})
}
