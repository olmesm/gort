package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

type CreateShortUrlBody struct {
	LongUrl         string     `json:"longUrl"`
	CustomSlug      *string    `json:"customSlug"`
	ShortCodeLength *int       `json:"shortCodeLength"`
	Domain          *string    `json:"domain"`
	Title           *string    `json:"title"`
	Tags            []string   `json:"tags"`
	Group           *string    `json:"group"`
	MaxVisits       *int64     `json:"maxVisits"`
	ValidSince      *time.Time `json:"validSince"`
	ValidUntil      *time.Time `json:"validUntil"`
	ForwardQuery    *bool      `json:"forwardQuery"`
	Crawlable       *bool      `json:"crawlable"`
	RedirectStatus  *int       `json:"redirectStatus"`
	FindIfExists    *bool      `json:"findIfExists"`
}

// EditShortUrlBody is the PATCH body: absent = leave unchanged; explicit null
// clears optional fields.
type EditShortUrlBody struct {
	LongUrl        Field[string]    `json:"longUrl"`
	Title          Field[string]    `json:"title"`
	Tags           Field[[]string]  `json:"tags"`
	Group          Field[string]    `json:"group"`
	MaxVisits      Field[int64]     `json:"maxVisits"`
	ValidSince     Field[time.Time] `json:"validSince"`
	ValidUntil     Field[time.Time] `json:"validUntil"`
	ForwardQuery   Field[bool]      `json:"forwardQuery"`
	Crawlable      Field[bool]      `json:"crawlable"`
	RedirectStatus Field[int]       `json:"redirectStatus"`
}

type RuleConditionBody struct {
	Type       string  `json:"type"`
	MatchKey   *string `json:"matchKey,omitempty"`
	MatchValue string  `json:"matchValue"`
}

type RuleBody struct {
	LongUrl    string              `json:"longUrl"`
	Conditions []RuleConditionBody `json:"conditions"`
}

type SetRulesBody struct {
	RedirectRules []RuleBody `json:"redirectRules"`
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
	longUrl, err := core.NewLongUrl(rule.LongUrl)
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
	return core.RedirectRule{Priority: index + 1, LongUrl: longUrl.Value(), Conditions: conditions}, nil
}

// isShortUrlUserError reports whether the error is one of the domain's
// user-facing categories (bad input), as opposed to an infrastructure
// failure.
func isShortUrlUserError(err error) bool {
	for _, category := range []error{
		core.ErrInvalidLongUrl, core.ErrInvalidSlug, core.ErrInvalidTag,
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
func shortUrlProblem(err error) error {
	switch {
	case errors.Is(err, core.ErrSlugInUse):
		return Conflict("non-unique-slug", err.Error())
	case errors.Is(err, core.ErrCodeGenerationExhausted):
		return NewProblem(500, "code-generation", "Could not generate a short code", err.Error())
	case isShortUrlUserError(err):
		return BadRequest(err.Error())
	default:
		return err
	}
}

// GET /rest/v1/short-urls
func (a *App) apiListShortUrls(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	// An unknown ?domain= filter matches nothing (-1 is an impossible id).
	var domainFilter *core.DomainID
	if authority := q.Get("domain"); authority != "" {
		d, err := data.DomainByAuthority(a.Db, strings.ToLower(authority))
		if err != nil {
			return err
		}
		id := core.DomainID(-1)
		if d != nil {
			id = core.DomainID(d.Id)
		}
		domainFilter = &id
	}

	orderBy, descending := data.OrderDateCreated, true
	if value := q.Get("orderBy"); value != "" {
		field, dir := value, "ASC"
		if parts := strings.Split(value, "-"); len(parts) == 2 {
			field, dir = parts[0], strings.ToUpper(parts[1])
		}
		switch field {
		case "shortCode":
			orderBy = data.OrderShortCode
		case "longUrl":
			orderBy = data.OrderLongUrl
		case "title":
			orderBy = data.OrderTitle
		case "visits":
			orderBy = data.OrderVisits
		default:
			orderBy = data.OrderDateCreated
		}
		descending = dir == "DESC"
	}

	var groupFilter *string
	if q.Has("group") {
		group := core.NormalizeGroup(q.Get("group"))
		groupFilter = &group
	}

	filters := data.ShortUrlFilters{
		SearchTerm:              q.Get("searchTerm"),
		Tags:                    queryStringList(q, "tags"),
		Group:                   groupFilter,
		TagsMatchAll:            strings.ToLower(q.Get("tagsMode")) == "all",
		StartDate:               queryDate(q, "startDate"),
		EndDate:                 queryDate(q, "endDate"),
		DomainId:                domainFilter,
		ExcludeMaxVisitsReached: queryBool(q, "excludeMaxVisitsReached"),
		ExcludePastValidUntil:   queryBool(q, "excludePastValidUntil"),
		OrderBy:                 orderBy,
		Descending:              descending,
		Page:                    queryIntDefault(q, "page", 1),
		ItemsPerPage:            queryIntDefault(q, "itemsPerPage", core.DefaultPageSize),
	}
	filters = applyKeyScope(key, filters)

	page, err := data.ListShortUrls(a.Db, filters)
	if err != nil {
		return err
	}
	ids := make([]core.ShortUrlID, len(page.Items))
	for i, d := range page.Items {
		ids[i] = core.ShortUrlID(d.Id)
	}
	tagsByUrl, err := data.TagsForShortUrls(a.Db, ids)
	if err != nil {
		return err
	}

	dto := NewPageDto(page, func(d data.ShortUrlDetail) ShortUrlDto {
		return NewShortUrlDto(a.Cfg, tagsByUrl[d.Id], &d)
	})
	return RespondJSON(w, http.StatusOK, dto)
}

// POST /rest/v1/short-urls
func (a *App) apiCreateShortUrl(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[CreateShortUrlBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}

	// Domain-scoped keys may only create URLs on their own domain.
	if key.Role.Kind == core.RoleDomain {
		d, err := data.DomainByID(a.Db, key.Role.DomainID)
		if err != nil {
			return err
		}
		allowed := d != nil && body.Domain != nil && strings.ToLower(*body.Domain) == d.Authority
		if !allowed {
			return Forbidden("This API key may only create short URLs on its own domain.")
		}
	}

	findIfExists := body.FindIfExists != nil && *body.FindIfExists
	spec, err := core.NewShortUrlSpec(core.ShortUrlSpecInput{
		LongUrl:        body.LongUrl,
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
		return shortUrlProblem(err)
	}

	dto, err := a.CreateShortUrl(ApiKeyAuthor(key.Id()), spec)
	if err != nil {
		return shortUrlProblem(err)
	}
	return RespondJSON(w, http.StatusCreated, dto)
}

// GET /rest/v1/short-urls/{code}
func (a *App) apiGetShortUrl(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}
	tags, err := data.TagsForShortUrl(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewShortUrlDto(a.Cfg, tags, detail))
}

// PATCH /rest/v1/short-urls/{code} — merge with current state, then validate
// the merged result as a whole through the edit spec.
func (a *App) apiEditShortUrl(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[EditShortUrlBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}

	input := core.ShortUrlEditInput{
		LongUrl:        body.LongUrl.PickValue(detail.LongUrl),
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

	edit, err := core.NewShortUrlEdit(input)
	if err != nil {
		return shortUrlProblem(err)
	}
	dto, err := a.EditShortUrl(core.ShortUrlID(detail.Id), detail, edit)
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, dto)
}

// DELETE /rest/v1/short-urls/{code}
func (a *App) apiDeleteShortUrl(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}
	if _, err := data.DeleteShortUrl(a.Db, core.ShortUrlID(detail.Id)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type rulesDto struct {
	DefaultLongUrl string        `json:"defaultLongUrl"`
	RedirectRules  []ruleItemDto `json:"redirectRules"`
}

type ruleItemDto struct {
	LongUrl    string              `json:"longUrl"`
	Priority   int                 `json:"priority"`
	Conditions []RuleConditionBody `json:"conditions"`
}

func newRulesDto(detail *data.ShortUrlDetail, rules []core.RedirectRule) rulesDto {
	items := make([]ruleItemDto, len(rules))
	for i, rule := range rules {
		conditions := make([]RuleConditionBody, len(rule.Conditions))
		for j, c := range rule.Conditions {
			conditions[j] = conditionToBody(c)
		}
		items[i] = ruleItemDto{LongUrl: rule.LongUrl, Priority: rule.Priority, Conditions: conditions}
	}
	return rulesDto{DefaultLongUrl: detail.LongUrl, RedirectRules: items}
}

// GET /rest/v1/short-urls/{code}/redirect-rules
func (a *App) apiGetRules(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}
	rules, err := data.RedirectRules(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, newRulesDto(detail, rules))
}

// POST /rest/v1/short-urls/{code}/redirect-rules — replaces all rules.
func (a *App) apiSetRules(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	body, err := ReadJSON[SetRulesBody](w, r)
	if err != nil {
		return BadRequest(err.Error())
	}
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}

	rules := make([]core.RedirectRule, len(body.RedirectRules))
	for i, rule := range body.RedirectRules {
		parsed, err := parseRule(i, rule)
		if err != nil {
			return BadRequest(err.Error())
		}
		rules[i] = parsed
	}

	if err := data.SetRedirectRules(a.Db, core.ShortUrlID(detail.Id), rules); err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, newRulesDto(detail, rules))
}

// GET /rest/v1/short-urls/{code}/visits
func (a *App) apiListShortUrlVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	q := r.URL.Query()
	detail, err := a.findAccessibleShortUrl(key, code, q.Get("domain"))
	if err != nil {
		return err
	}
	page, err := data.ListVisitsForShortUrl(a.Db, core.ShortUrlID(detail.Id), visitFiltersFromQuery(q))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, NewPageDto(page, NewVisitDto))
}

// DELETE /rest/v1/short-urls/{code}/visits
func (a *App) apiDeleteShortUrlVisits(key *AuthenticatedKey, w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("code")
	detail, err := a.findAccessibleShortUrl(key, code, r.URL.Query().Get("domain"))
	if err != nil {
		return err
	}
	deleted, err := data.DeleteVisitsForShortUrl(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		return err
	}
	return RespondJSON(w, http.StatusOK, map[string]int{"deletedVisits": deleted})
}
