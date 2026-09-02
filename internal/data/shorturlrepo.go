package data

import (
	"database/sql"
	"errors"
	"fmt"
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

func detailSelect(db *Db) string {
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

type rowScanner interface{ Scan(dest ...any) error }

func scanShortUrlDetail(r rowScanner) (*ShortUrlDetail, error) {
	var d ShortUrlDetail
	var title, groupName sql.NullString
	var maxVisits, authorUserId, authorApiKeyId sql.NullInt64
	var validSince, validUntil, createdAt NullTime
	err := r.Scan(&d.Id, &d.ShortCode, &d.DomainId, &d.Authority, &d.LongUrl, &title,
		&d.TitleWasAutoResolved, &d.RedirectStatus, &d.ForwardQuery, &d.Crawlable,
		&maxVisits, &validSince, &validUntil, &authorUserId, &authorApiKeyId,
		&groupName, &createdAt, &d.VisitCount, &d.BotVisitCount)
	if err != nil {
		return nil, err
	}
	d.Title = strPtr(title)
	d.GroupName = strPtr(groupName)
	d.MaxVisits = int64Ptr(maxVisits)
	d.ValidSince = validSince.Ptr()
	d.ValidUntil = validUntil.Ptr()
	d.AuthorUserId = int64Ptr(authorUserId)
	d.AuthorApiKeyId = int64Ptr(authorApiKeyId)
	d.CreatedAt = createdAt.Time
	return &d, nil
}

func scanShortUrlRow(r rowScanner) (*ShortUrlRow, error) {
	var s ShortUrlRow
	var title, groupName sql.NullString
	var maxVisits, authorUserId, authorApiKeyId sql.NullInt64
	var validSince, validUntil, createdAt NullTime
	err := r.Scan(&s.Id, &s.ShortCode, &s.DomainId, &s.LongUrl, &title, &s.TitleWasAutoResolved,
		&s.RedirectStatus, &s.ForwardQuery, &s.Crawlable, &maxVisits, &validSince,
		&validUntil, &authorUserId, &authorApiKeyId, &groupName, &createdAt)
	if err != nil {
		return nil, err
	}
	s.Title = strPtr(title)
	s.GroupName = strPtr(groupName)
	s.MaxVisits = int64Ptr(maxVisits)
	s.ValidSince = validSince.Ptr()
	s.ValidUntil = validUntil.Ptr()
	s.AuthorUserId = int64Ptr(authorUserId)
	s.AuthorApiKeyId = int64Ptr(authorApiKeyId)
	s.CreatedAt = createdAt.Time
	return &s, nil
}

func insertTagLinks(tx *Tx, shortUrlId int64, tags []core.TagName) error {
	for _, tag := range tags {
		if _, err := tx.Exec(
			"INSERT INTO tags (name) VALUES (?) ON CONFLICT (name) DO NOTHING", tag.Value()); err != nil {
			return err
		}
		if _, err := tx.Exec(
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
func CreateShortUrl(db *Db, nu NewShortUrl, tags []core.TagName) (core.ShortUrlID, error) {
	var id int64
	err := db.WithTx(func(tx *Tx) error {
		var authorUserId, authorApiKeyId any
		if nu.AuthorUserId != nil {
			authorUserId = nu.AuthorUserId.Value()
		}
		if nu.AuthorApiKeyId != nil {
			authorApiKeyId = nu.AuthorApiKeyId.Value()
		}
		err := tx.QueryRow(
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
		return insertTagLinks(tx, id, tags)
	})
	if err != nil {
		if IsDuplicateKey(err) {
			return 0, ErrDuplicateShortCode
		}
		return 0, err
	}
	return core.ShortUrlID(id), nil
}

// TryGetByCode looks up by a *candidate* code from the URL path — untrusted
// input, so a plain string is the honest parameter type here. Returns nil
// when not found.
func TryGetByCode(db *Db, domainId core.DomainID, code string) (*ShortUrlRow, error) {
	row := db.QueryRow(
		`SELECT id, short_code, domain_id, long_url, title, title_was_auto_resolved,
		        redirect_status, forward_query, crawlable, max_visits, valid_since,
		        valid_until, author_user_id, author_api_key_id, group_name, created_at
		 FROM short_urls WHERE domain_id = ? AND short_code = ?`,
		domainId.Value(), code)
	s, err := scanShortUrlRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

func TryGetDetail(db *Db, domainId core.DomainID, code string) (*ShortUrlDetail, error) {
	row := db.QueryRow(detailSelect(db)+" WHERE su.domain_id = ? AND su.short_code = ?",
		domainId.Value(), code)
	d, err := scanShortUrlDetail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func TryFindByLongUrl(db *Db, domainId core.DomainID, longUrl core.LongUrl) (*ShortUrlDetail, error) {
	row := db.QueryRow(detailSelect(db)+" WHERE su.domain_id = ? AND su.long_url = ? ORDER BY su.id LIMIT 1",
		domainId.Value(), longUrl.Value())
	d, err := scanShortUrlDetail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func TryGetDetailById(db *Db, id core.ShortUrlID) (*ShortUrlDetail, error) {
	row := db.QueryRow(detailSelect(db)+" WHERE su.id = ?", id.Value())
	d, err := scanShortUrlDetail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func UpdateShortUrl(db *Db, id core.ShortUrlID, u ShortUrlUpdate) (bool, error) {
	res, err := db.Exec(
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
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

// SetShortUrlTags replaces the tag set of an existing short URL, atomically.
func SetShortUrlTags(db *Db, id core.ShortUrlID, tags []core.TagName) error {
	return db.WithTx(func(tx *Tx) error {
		if _, err := tx.Exec("DELETE FROM short_url_tags WHERE short_url_id = ?", id.Value()); err != nil {
			return err
		}
		return insertTagLinks(tx, id.Value(), tags)
	})
}

func SetResolvedTitle(db *Db, id core.ShortUrlID, title string) error {
	_, err := db.Exec(
		`UPDATE short_urls SET title = ?, title_was_auto_resolved = ?
		 WHERE id = ? AND title IS NULL`,
		title, true, id.Value())
	return err
}

func DeleteShortUrl(db *Db, id core.ShortUrlID) (bool, error) {
	res, err := db.Exec("DELETE FROM short_urls WHERE id = ?", id.Value())
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

// ListMissingTitles lists short URLs that still need automatic title
// resolution.
func ListMissingTitles(db *Db, limit int) ([]struct {
	Id      core.ShortUrlID
	LongUrl string
}, error) {
	rows, err := db.Query(
		`SELECT id, long_url FROM short_urls WHERE title IS NULL ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Id      core.ShortUrlID
		LongUrl string
	}
	for rows.Next() {
		var id int64
		var url string
		if err := rows.Scan(&id, &url); err != nil {
			return nil, err
		}
		out = append(out, struct {
			Id      core.ShortUrlID
			LongUrl string
		}{core.ShortUrlID(id), url})
	}
	return out, rows.Err()
}

// ListCrawlable lists all crawlable short URL codes, for robots.txt generation.
func ListCrawlable(db *Db) ([]string, error) {
	rows, err := db.Query(
		fmt.Sprintf("SELECT short_code FROM short_urls WHERE crawlable = %s ORDER BY short_code",
			db.BoolLiteral(true)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// ListGroupNames lists the distinct groups referenced by short URLs, for
// group pickers and filters.
func ListGroupNames(db *Db) ([]string, error) {
	rows, err := db.Query(
		"SELECT DISTINCT group_name FROM short_urls WHERE group_name IS NOT NULL ORDER BY group_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func CountValidVisits(db *Db, id core.ShortUrlID) (int64, error) {
	var count int64
	err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM visits v WHERE v.short_url_id = ? AND %s", validVisitExpr()),
		id.Value()).Scan(&count)
	return count, err
}

func ListShortUrls(db *Db, filters ShortUrlFilters) (core.Page[ShortUrlDetail], error) {
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

	var total int64
	err := db.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM short_urls su
		             JOIN domains d ON d.id = su.domain_id %s`, whereClause),
		args...).Scan(&total)
	if err != nil {
		return empty, err
	}

	listArgs := append(append([]any{}, args...), size, core.PageOffset(page, size))
	rows, err := db.Query(
		fmt.Sprintf(`%s %s ORDER BY %s %s, su.id %s LIMIT ? OFFSET ?`,
			detailSelect(db), whereClause, orderCol, orderDir, orderDir),
		listArgs...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()

	items := []ShortUrlDetail{}
	for rows.Next() {
		d, err := scanShortUrlDetail(rows)
		if err != nil {
			return empty, err
		}
		items = append(items, *d)
	}
	if err := rows.Err(); err != nil {
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

func GetRules(db *Db, shortUrlId core.ShortUrlID) ([]core.RedirectRule, error) {
	rows, err := db.Query(
		`SELECT id, priority, long_url FROM redirect_rules
		 WHERE short_url_id = ? ORDER BY priority`, shortUrlId.Value())
	if err != nil {
		return nil, err
	}
	type ruleRow struct {
		id       int64
		priority int
		longUrl  string
	}
	var ruleRows []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.id, &r.priority, &r.longUrl); err != nil {
			rows.Close()
			return nil, err
		}
		ruleRows = append(ruleRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ruleRows) == 0 {
		return []core.RedirectRule{}, nil
	}

	ids := make([]int64, len(ruleRows))
	for i, r := range ruleRows {
		ids[i] = r.id
	}
	inClause, inArgs := InList("rule_id", ids)
	condRows, err := db.Query(
		fmt.Sprintf(`SELECT rule_id, cond_type, match_key, match_value
		             FROM redirect_conditions WHERE %s`, inClause), inArgs...)
	if err != nil {
		return nil, err
	}
	condsByRule := map[int64][]core.RuleCondition{}
	for condRows.Next() {
		var ruleId int64
		var condType, matchValue string
		var matchKey sql.NullString
		if err := condRows.Scan(&ruleId, &condType, &matchKey, &matchValue); err != nil {
			condRows.Close()
			return nil, err
		}
		if cond, ok := parseConditionRow(condType, strPtr(matchKey), matchValue); ok {
			condsByRule[ruleId] = append(condsByRule[ruleId], cond)
		}
	}
	condRows.Close()
	if err := condRows.Err(); err != nil {
		return nil, err
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

// SetRules replaces all redirect rules of a short URL, atomically.
func SetRules(db *Db, shortUrlId core.ShortUrlID, rules []core.RedirectRule) error {
	sorted := make([]core.RedirectRule, len(rules))
	copy(sorted, rules)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Priority < sorted[j-1].Priority; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return db.WithTx(func(tx *Tx) error {
		if _, err := tx.Exec("DELETE FROM redirect_rules WHERE short_url_id = ?", shortUrlId.Value()); err != nil {
			return err
		}
		for i, rule := range sorted {
			var ruleId int64
			err := tx.QueryRow(
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
				if _, err := tx.Exec(
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
