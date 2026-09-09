package data

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
)

// NewShortUrl is the write model for a new short URL. Fields that carry
// domain meaning are the validated domain types; the repository unwraps them
// at the SQL boundary.
type NewShortUrl struct {
	ShortCode      core.ShortCode
	DomainId       core.DomainID
	LongUrl        core.LongUrl
	Title          *string
	RedirectStatus core.RedirectStatus
	ForwardQuery   bool
	Crawlable      bool
	Lifetime       core.Lifetime
	AuthorUserId   *core.UserID
	AuthorApiKeyId *core.ApiKeyID
	// GroupName scopes who can see and manage the link; nil = ungrouped.
	GroupName *string
}

// ShortUrlUpdate carries the final values for every editable field of a
// short URL.
type ShortUrlUpdate struct {
	LongUrl              core.LongUrl
	Title                *string
	TitleWasAutoResolved bool
	RedirectStatus       core.RedirectStatus
	ForwardQuery         bool
	Crawlable            bool
	Lifetime             core.Lifetime
	GroupName            *string
}

type ShortUrlOrder int

const (
	OrderDateCreated ShortUrlOrder = iota
	OrderShortCode
	OrderLongUrl
	OrderTitle
	OrderVisits
)

type ShortUrlFilters struct {
	SearchTerm     string
	Tags           []string
	TagsMatchAll   bool
	StartDate      *time.Time
	EndDate        *time.Time
	DomainId       *core.DomainID
	AuthorApiKeyId *core.ApiKeyID
	// Group filters to one exact group when non-nil ("" = ungrouped only).
	Group *string
	// VisibleGroups, when non-nil, restricts results to ungrouped links plus
	// links in one of these groups — the authorization scope for non-admin
	// dashboard users. nil = unrestricted.
	VisibleGroups           []string
	ExcludeMaxVisitsReached bool
	ExcludePastValidUntil   bool
	OrderBy                 ShortUrlOrder
	Descending              bool
	Page                    int
	ItemsPerPage            int
}

func EmptyShortUrlFilters() ShortUrlFilters {
	return ShortUrlFilters{
		OrderBy:      OrderDateCreated,
		Descending:   true,
		Page:         1,
		ItemsPerPage: core.DefaultPageSize,
	}
}

// ErrDuplicateShortCode is returned when the (domain, code) pair already exists.
var ErrDuplicateShortCode = errors.New("duplicate short code")

func validVisitExpr() string { return IsValidVisit("v") }

func visitCountExpr() string {
	return fmt.Sprintf("(SELECT COUNT(*) FROM visits v WHERE v.short_url_id = su.id AND %s)", validVisitExpr())
}

func detailSelect(ctx context.Context, db *Db) string {
	botCount := fmt.Sprintf(
		"(SELECT COUNT(*) FROM visits v WHERE v.short_url_id = su.id AND %s AND v.is_bot = %s)",
		validVisitExpr(), db.BoolLiteral(true))
	return fmt.Sprintf(`SELECT su.id, su.short_code, su.domain_id, d.authority, su.long_url, su.title,
	         su.title_was_auto_resolved, su.redirect_status, su.forward_query, su.crawlable,
	         su.max_visits, su.valid_since, su.valid_until, su.author_user_id, su.author_api_key_id,
	         su.group_name, su.created_at,
	         %s AS visit_count,
	         %s AS bot_visit_count
	  FROM short_urls su
	  JOIN domains d ON d.id = su.domain_id`, visitCountExpr(), botCount)
}

func scanShortUrlDetail(r rowScanner) (*ShortUrlDetail, error) {
	var d ShortUrlDetail
	err := r.Scan(&d.Id, &d.ShortCode, &d.DomainId, &d.Authority, &d.LongUrl, &d.Title,
		&d.TitleWasAutoResolved, &d.RedirectStatus, &d.ForwardQuery, &d.Crawlable,
		&d.MaxVisits, asTimePtr(&d.ValidSince), asTimePtr(&d.ValidUntil), &d.AuthorUserId, &d.AuthorApiKeyId,
		&d.GroupName, asTime(&d.CreatedAt), &d.VisitCount, &d.BotVisitCount)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func scanShortUrlRow(r rowScanner) (*ShortUrlRow, error) {
	var s ShortUrlRow
	err := r.Scan(&s.Id, &s.ShortCode, &s.DomainId, &s.LongUrl, &s.Title, &s.TitleWasAutoResolved,
		&s.RedirectStatus, &s.ForwardQuery, &s.Crawlable, &s.MaxVisits, asTimePtr(&s.ValidSince),
		asTimePtr(&s.ValidUntil), &s.AuthorUserId, &s.AuthorApiKeyId, &s.GroupName, asTime(&s.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func insertTagLinks(ctx context.Context, tx *Tx, shortUrlId int64, tags []core.TagName) error {
	for _, tag := range tags {
		if _, err := tx.Exec(ctx,
			"INSERT INTO tags (name) VALUES (?) ON CONFLICT (name) DO NOTHING", tag.Value()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO short_url_tags (short_url_id, tag_id)
			 SELECT ?, id FROM tags WHERE name = ?
			 ON CONFLICT (short_url_id, tag_id) DO NOTHING`,
			shortUrlId, tag.Value()); err != nil {
			return err
		}
	}
	return nil
}

// CreateShortUrl atomically inserts a short URL together with its tag links.
// The tag links belong to the short URL's consistency boundary, which is why
// this repository owns the whole write: a short URL is never observable
// half-created. Returns ErrDuplicateShortCode when the (domain, code) pair
// exists.
func CreateShortUrl(ctx context.Context, db *Db, nu NewShortUrl, tags []core.TagName) (core.ShortUrlID, error) {
	var id int64
	err := db.WithTx(ctx, func(tx *Tx) error {
		var authorUserId, authorApiKeyId any
		if nu.AuthorUserId != nil {
			authorUserId = nu.AuthorUserId.Value()
		}
		if nu.AuthorApiKeyId != nil {
			authorApiKeyId = nu.AuthorApiKeyId.Value()
		}
		err := tx.QueryRow(ctx,
			`INSERT INTO short_urls
			   (short_code, domain_id, long_url, title, title_was_auto_resolved,
			    redirect_status, forward_query, crawlable, max_visits,
			    valid_since, valid_until, author_user_id, author_api_key_id, group_name, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 RETURNING id`,
			nu.ShortCode.Value(), nu.DomainId.Value(), nu.LongUrl.Value(), nu.Title, false,
			nu.RedirectStatus.Code(), nu.ForwardQuery, nu.Crawlable, nu.Lifetime.MaxVisits,
			db.BindTimePtr(nu.Lifetime.ValidSince), db.BindTimePtr(nu.Lifetime.ValidUntil),
			authorUserId, authorApiKeyId, nu.GroupName, db.BindTime(time.Now())).Scan(&id)
		if err != nil {
			return err
		}
		return insertTagLinks(ctx, tx, id, tags)
	})
	if err != nil {
		if IsDuplicateKey(err) {
			return 0, ErrDuplicateShortCode
		}
		return 0, err
	}
	return core.ShortUrlID(id), nil
}

// ShortUrlByCode looks up by a *candidate* code from the URL path — untrusted
// input, so a plain string is the honest parameter type here. Returns nil
// when not found.
func ShortUrlByCode(ctx context.Context, db *Db, domainId core.DomainID, code string) (*ShortUrlRow, error) {
	return queryOne(ctx, db, scanShortUrlRow,
		`SELECT id, short_code, domain_id, long_url, title, title_was_auto_resolved,
		        redirect_status, forward_query, crawlable, max_visits, valid_since,
		        valid_until, author_user_id, author_api_key_id, group_name, created_at
		 FROM short_urls WHERE domain_id = ? AND short_code = ?`,
		domainId.Value(), code)
}

func ShortUrlDetailByCode(ctx context.Context, db *Db, domainId core.DomainID, code string) (*ShortUrlDetail, error) {
	return queryOne(ctx, db, scanShortUrlDetail,
		detailSelect(ctx, db)+" WHERE su.domain_id = ? AND su.short_code = ?",
		domainId.Value(), code)
}

func ShortUrlDetailByLongUrl(ctx context.Context, db *Db, domainId core.DomainID, longUrl core.LongUrl) (*ShortUrlDetail, error) {
	return queryOne(ctx, db, scanShortUrlDetail,
		detailSelect(ctx, db)+" WHERE su.domain_id = ? AND su.long_url = ? ORDER BY su.id LIMIT 1",
		domainId.Value(), longUrl.Value())
}

func ShortUrlDetailByID(ctx context.Context, db *Db, id core.ShortUrlID) (*ShortUrlDetail, error) {
	return queryOne(ctx, db, scanShortUrlDetail, detailSelect(ctx, db)+" WHERE su.id = ?", id.Value())
}

func UpdateShortUrl(ctx context.Context, db *Db, id core.ShortUrlID, u ShortUrlUpdate) (bool, error) {
	return execAffected(ctx, db,
		`UPDATE short_urls SET
		   long_url = ?, title = ?, title_was_auto_resolved = ?,
		   redirect_status = ?, forward_query = ?,
		   crawlable = ?, max_visits = ?,
		   valid_since = ?, valid_until = ?, group_name = ?
		 WHERE id = ?`,
		u.LongUrl.Value(), u.Title, u.TitleWasAutoResolved,
		u.RedirectStatus.Code(), u.ForwardQuery, u.Crawlable, u.Lifetime.MaxVisits,
		db.BindTimePtr(u.Lifetime.ValidSince), db.BindTimePtr(u.Lifetime.ValidUntil),
		u.GroupName, id.Value())
}

// SetShortUrlTags replaces the tag set of an existing short URL, atomically.
func SetShortUrlTags(ctx context.Context, db *Db, id core.ShortUrlID, tags []core.TagName) error {
	return db.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM short_url_tags WHERE short_url_id = ?", id.Value()); err != nil {
			return err
		}
		return insertTagLinks(ctx, tx, id.Value(), tags)
	})
}

func SetResolvedTitle(ctx context.Context, db *Db, id core.ShortUrlID, title string) error {
	_, err := db.Exec(ctx,
		`UPDATE short_urls SET title = ?, title_was_auto_resolved = ?
		 WHERE id = ? AND title IS NULL`,
		title, true, id.Value())
	return err
}

func DeleteShortUrl(ctx context.Context, db *Db, id core.ShortUrlID) (bool, error) {
	return execAffected(ctx, db, "DELETE FROM short_urls WHERE id = ?", id.Value())
}

// MissingTitleRow is a short URL that still needs automatic title resolution.
type MissingTitleRow struct {
	Id      core.ShortUrlID
	LongUrl string
}

func ListMissingTitles(ctx context.Context, db *Db, limit int) ([]MissingTitleRow, error) {
	return queryAll(ctx, db, func(r rowScanner) (*MissingTitleRow, error) {
		var m MissingTitleRow
		if err := r.Scan(&m.Id, &m.LongUrl); err != nil {
			return nil, err
		}
		return &m, nil
	}, `SELECT id, long_url FROM short_urls WHERE title IS NULL ORDER BY id DESC LIMIT ?`, limit)
}

// ListCrawlable lists all crawlable short URL codes, for robots.txt generation.
func ListCrawlable(ctx context.Context, db *Db) ([]string, error) {
	return queryStrings(ctx, db,
		fmt.Sprintf("SELECT short_code FROM short_urls WHERE crawlable = %s ORDER BY short_code",
			db.BoolLiteral(true)))
}

// ListGroupNames lists the distinct groups referenced by short URLs, for
// group pickers and filters.
func ListGroupNames(ctx context.Context, db *Db) ([]string, error) {
	return queryStrings(ctx, db,
		"SELECT DISTINCT group_name FROM short_urls WHERE group_name IS NOT NULL ORDER BY group_name")
}

func CountValidVisits(ctx context.Context, db *Db, id core.ShortUrlID) (int64, error) {
	return queryScalar[int64](ctx, db,
		fmt.Sprintf("SELECT COUNT(*) FROM visits v WHERE v.short_url_id = ? AND %s", validVisitExpr()),
		id.Value())
}

func ListShortUrls(ctx context.Context, db *Db, filters ShortUrlFilters) (core.Page[ShortUrlDetail], error) {
	empty := core.Page[ShortUrlDetail]{}
	page, size := core.NormalizePaging(filters.Page, filters.ItemsPerPage)
	var conditions []string
	var args []any

	if term := strings.TrimSpace(filters.SearchTerm); term != "" {
		search := "%" + term + "%"
		like := func(col string) string { return db.ILike(col, "?") }
		conditions = append(conditions, fmt.Sprintf(
			`(%s OR %s OR %s OR %s
			  OR EXISTS (SELECT 1 FROM short_url_tags st JOIN tags t ON t.id = st.tag_id
			             WHERE st.short_url_id = su.id AND %s))`,
			like("su.long_url"), like("coalesce(su.title, '')"), like("su.short_code"),
			like("d.authority"), like("t.name")))
		args = append(args, search, search, search, search, search)
	}

	if len(filters.Tags) > 0 {
		tagsIn, tagArgs := InList("t.name", filters.Tags)
		if filters.TagsMatchAll {
			conditions = append(conditions, fmt.Sprintf(
				`(SELECT COUNT(DISTINCT t.name) FROM short_url_tags st
				  JOIN tags t ON t.id = st.tag_id
				  WHERE st.short_url_id = su.id AND %s) = ?`, tagsIn))
			args = append(args, tagArgs...)
			args = append(args, len(filters.Tags))
		} else {
			conditions = append(conditions, fmt.Sprintf(
				`EXISTS (SELECT 1 FROM short_url_tags st JOIN tags t ON t.id = st.tag_id
				         WHERE st.short_url_id = su.id AND %s)`, tagsIn))
			args = append(args, tagArgs...)
		}
	}

	if filters.StartDate != nil {
		conditions = append(conditions, "su.created_at >= ?")
		args = append(args, db.BindTime(*filters.StartDate))
	}
	if filters.EndDate != nil {
		conditions = append(conditions, "su.created_at <= ?")
		args = append(args, db.BindTime(*filters.EndDate))
	}
	if filters.DomainId != nil {
		conditions = append(conditions, "su.domain_id = ?")
		args = append(args, filters.DomainId.Value())
	}
	if filters.AuthorApiKeyId != nil {
		conditions = append(conditions, "su.author_api_key_id = ?")
		args = append(args, filters.AuthorApiKeyId.Value())
	}
	if filters.Group != nil {
		if *filters.Group == "" {
			conditions = append(conditions, "su.group_name IS NULL")
		} else {
			conditions = append(conditions, "su.group_name = ?")
			args = append(args, *filters.Group)
		}
	}
	if filters.VisibleGroups != nil {
		if len(filters.VisibleGroups) == 0 {
			conditions = append(conditions, "su.group_name IS NULL")
		} else {
			groupsIn, groupArgs := InList("su.group_name", filters.VisibleGroups)
			conditions = append(conditions, fmt.Sprintf("(su.group_name IS NULL OR %s)", groupsIn))
			args = append(args, groupArgs...)
		}
	}
	if filters.ExcludeMaxVisitsReached {
		conditions = append(conditions,
			fmt.Sprintf("(su.max_visits IS NULL OR %s < su.max_visits)", visitCountExpr()))
	}
	if filters.ExcludePastValidUntil {
		conditions = append(conditions, "(su.valid_until IS NULL OR su.valid_until >= ?)")
		args = append(args, db.BindTime(time.Now()))
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderCol := "su.created_at"
	switch filters.OrderBy {
	case OrderShortCode:
		orderCol = "su.short_code"
	case OrderLongUrl:
		orderCol = "su.long_url"
	case OrderTitle:
		orderCol = "su.title"
	case OrderVisits:
		orderCol = "visit_count"
	}
	orderDir := "ASC"
	if filters.Descending {
		orderDir = "DESC"
	}

	total, err := queryScalar[int64](ctx, db,
		fmt.Sprintf(`SELECT COUNT(*) FROM short_urls su
		             JOIN domains d ON d.id = su.domain_id %s`, whereClause),
		args...)
	if err != nil {
		return empty, err
	}

	listArgs := append(append([]any{}, args...), size, core.PageOffset(page, size))
	items, err := queryAll(ctx, db, scanShortUrlDetail,
		fmt.Sprintf(`%s %s ORDER BY %s %s, su.id %s LIMIT ? OFFSET ?`,
			detailSelect(ctx, db), whereClause, orderCol, orderDir, orderDir),
		listArgs...)
	if err != nil {
		return empty, err
	}

	return core.Page[ShortUrlDetail]{
		Items:        items,
		CurrentPage:  page,
		ItemsPerPage: size,
		TotalItems:   total,
	}, nil
}

// ---- Redirect rules ----

func parseConditionRow(condType string, matchKey *string, matchValue string) (core.RuleCondition, bool) {
	switch condType {
	case "device":
		if d, ok := core.DeviceOfSlug(matchValue); ok {
			return core.DeviceIs(d), true
		}
		return core.RuleCondition{}, false
	case "language":
		return core.LanguageIs(matchValue), true
	case "query-param":
		if matchKey != nil {
			return core.QueryParamIs(*matchKey, matchValue), true
		}
		return core.RuleCondition{}, false
	case "ip-address":
		return core.IPInRange(matchValue), true
	default:
		return core.RuleCondition{}, false
	}
}

func RedirectRules(ctx context.Context, db *Db, shortUrlId core.ShortUrlID) ([]core.RedirectRule, error) {
	type ruleRow struct {
		id       int64
		priority int
		longUrl  string
	}
	ruleRows, err := queryAll(ctx, db, func(r rowScanner) (*ruleRow, error) {
		var row ruleRow
		if err := r.Scan(&row.id, &row.priority, &row.longUrl); err != nil {
			return nil, err
		}
		return &row, nil
	}, `SELECT id, priority, long_url FROM redirect_rules
	    WHERE short_url_id = ? ORDER BY priority`, shortUrlId.Value())
	if err != nil {
		return nil, err
	}
	if len(ruleRows) == 0 {
		return []core.RedirectRule{}, nil
	}

	ids := make([]int64, len(ruleRows))
	for i, r := range ruleRows {
		ids[i] = r.id
	}
	type condRow struct {
		ruleId int64
		cond   core.RuleCondition
		ok     bool
	}
	inClause, inArgs := InList("rule_id", ids)
	condRows, err := queryAll(ctx, db, func(r rowScanner) (*condRow, error) {
		var ruleId int64
		var condType, matchValue string
		var matchKey *string
		if err := r.Scan(&ruleId, &condType, &matchKey, &matchValue); err != nil {
			return nil, err
		}
		cond, ok := parseConditionRow(condType, matchKey, matchValue)
		return &condRow{ruleId: ruleId, cond: cond, ok: ok}, nil
	}, fmt.Sprintf(`SELECT rule_id, cond_type, match_key, match_value
	                FROM redirect_conditions WHERE %s`, inClause), inArgs...)
	if err != nil {
		return nil, err
	}
	condsByRule := map[int64][]core.RuleCondition{}
	for _, c := range condRows {
		if c.ok {
			condsByRule[c.ruleId] = append(condsByRule[c.ruleId], c.cond)
		}
	}

	rules := make([]core.RedirectRule, len(ruleRows))
	for i, r := range ruleRows {
		conds := condsByRule[r.id]
		if conds == nil {
			conds = []core.RuleCondition{}
		}
		rules[i] = core.RedirectRule{Priority: r.priority, LongUrl: r.longUrl, Conditions: conds}
	}
	return rules, nil
}

// SetRedirectRules replaces all redirect rules of a short URL, atomically.
func SetRedirectRules(ctx context.Context, db *Db, shortUrlId core.ShortUrlID, rules []core.RedirectRule) error {
	sorted := slices.Clone(rules)
	slices.SortStableFunc(sorted, func(a, b core.RedirectRule) int { return a.Priority - b.Priority })
	return db.WithTx(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, "DELETE FROM redirect_rules WHERE short_url_id = ?", shortUrlId.Value()); err != nil {
			return err
		}
		for i, rule := range sorted {
			var ruleId int64
			err := tx.QueryRow(ctx,
				`INSERT INTO redirect_rules (short_url_id, priority, long_url)
				 VALUES (?, ?, ?) RETURNING id`,
				shortUrlId.Value(), i+1, rule.LongUrl).Scan(&ruleId)
			if err != nil {
				return err
			}
			for _, cond := range rule.Conditions {
				var matchKey any
				if cond.Type == core.CondQueryParam {
					matchKey = cond.Key
				}
				if _, err := tx.Exec(ctx,
					`INSERT INTO redirect_conditions (rule_id, cond_type, match_key, match_value)
					 VALUES (?, ?, ?, ?)`,
					ruleId, string(cond.Type), matchKey, cond.Value); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
