package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
)

// ---- helpers ----

func parseDateLocal(s string) *time.Time {
	return TryParseDate(s)
}

func dateLocalValue(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.UTC().Format("2006-01-02T15:04")
}

type suListQuery struct {
	Search  string
	Tag     string
	Group   string
	Domain  string
	OrderBy string
	Dir     string
	Page    int
}

func readSuListQuery(q url.Values) suListQuery {
	orderBy := q.Get("orderBy")
	if orderBy == "" {
		orderBy = "dateCreated"
	}
	dir := q.Get("dir")
	if dir == "" {
		dir = "desc"
	}
	return suListQuery{
		Search:  q.Get("search"),
		Tag:     q.Get("tag"),
		Group:   q.Get("group"),
		Domain:  q.Get("domain"),
		OrderBy: orderBy,
		Dir:     dir,
		Page:    queryIntDefault(q, "page", 1),
	}
}

func suListUrl(lq suListQuery, page int) string {
	var parts []string
	if lq.Search != "" {
		parts = append(parts, "search="+url.QueryEscape(lq.Search))
	}
	if lq.Tag != "" {
		parts = append(parts, "tag="+url.QueryEscape(lq.Tag))
	}
	if lq.Group != "" {
		parts = append(parts, "group="+url.QueryEscape(lq.Group))
	}
	if lq.Domain != "" {
		parts = append(parts, "domain="+url.QueryEscape(lq.Domain))
	}
	if lq.OrderBy != "dateCreated" {
		parts = append(parts, "orderBy="+lq.OrderBy)
	}
	if lq.Dir != "desc" {
		parts = append(parts, "dir="+lq.Dir)
	}
	if page > 1 {
		parts = append(parts, fmt.Sprintf("page=%d", page))
	}
	if len(parts) == 0 {
		return "/admin/short-urls"
	}
	return "/admin/short-urls?" + strings.Join(parts, "&")
}

func suFiltersOf(lq suListQuery, user *CurrentUser) data.ShortUrlFilters {
	filters := data.EmptyShortUrlFilters()
	filters.SearchTerm = lq.Search
	if lq.Tag != "" {
		filters.Tags = []string{lq.Tag}
	}
	if group := core.NormalizeGroup(lq.Group); group != "" {
		filters.Group = &group
	}
	filters.VisibleGroups = user.VisibleGroups()
	switch lq.OrderBy {
	case "shortCode":
		filters.OrderBy = data.OrderShortCode
	case "longUrl":
		filters.OrderBy = data.OrderLongUrl
	case "title":
		filters.OrderBy = data.OrderTitle
	case "visits":
		filters.OrderBy = data.OrderVisits
	default:
		filters.OrderBy = data.OrderDateCreated
	}
	filters.Descending = lq.Dir != "asc"
	filters.Page = lq.Page
	filters.ItemsPerPage = 20
	return filters
}

// ---- list ----

type sortHeaderView struct {
	Url   string
	Label string
}

func sortHeader(lq suListQuery, field, label string) sortHeaderView {
	nextDir := "desc"
	if lq.OrderBy == field && lq.Dir == "desc" {
		nextDir = "asc"
	}
	next := lq
	next.OrderBy, next.Dir, next.Page = field, nextDir, 1

	marker := ""
	if lq.OrderBy == field {
		if lq.Dir == "asc" {
			marker = " ↑"
		} else {
			marker = " ↓"
		}
	}
	return sortHeaderView{Url: suListUrl(next, 1), Label: label + marker}
}

type suTableView struct {
	HeadShortCode sortHeaderView
	HeadTitle     sortHeaderView
	HeadLongUrl   sortHeaderView
	HeadVisits    sortHeaderView
	HeadCreated   sortHeaderView
	Rows          []suRowView
	Pager         pagerView
}

type suRowView struct {
	ShortUrl    string
	Display     string
	Title       string
	LongUrl     string
	Tags        []string
	Group       *string
	VisitsUrl   string
	VisitsLabel string
	Created     string
	EditUrl     string
}

func (a *App) suTable(lq suListQuery, page core.Page[data.ShortUrlDetail], tagsByUrl map[core.ShortUrlID][]string) suTableView {
	table := suTableView{
		HeadShortCode: sortHeader(lq, "shortCode", "Short URL"),
		HeadTitle:     sortHeader(lq, "title", "Title"),
		HeadLongUrl:   sortHeader(lq, "longUrl", "Long URL"),
		HeadVisits:    sortHeader(lq, "visits", "Visits"),
		HeadCreated:   sortHeader(lq, "dateCreated", "Created"),
		Pager:         newPager(page, func(p int) string { return suListUrl(lq, p) }),
	}
	for _, d := range page.Items {
		table.Rows = append(table.Rows, suRowView{
			ShortUrl:    ShortUrlFor(a.Cfg, d.Authority, d.ShortCode),
			Display:     d.Authority + "/" + d.ShortCode,
			Title:       orDash(d.Title),
			LongUrl:     d.LongUrl,
			Tags:        tagsByUrl[d.Id],
			Group:       d.GroupName,
			VisitsUrl:   fmt.Sprintf("/admin/short-urls/%d/visits", d.Id),
			VisitsLabel: formatCount(d.VisitCount),
			Created:     formatDateTime(d.CreatedAt),
			EditUrl:     fmt.Sprintf("/admin/short-urls/%d/edit", d.Id),
		})
	}
	return table
}

type suListView struct {
	Search       string
	TagOptions   []optionView
	GroupOptions []optionView
	Table        suTableView
}

// GET /admin/short-urls (full page or htmx fragment)
func (a *App) uiListShortUrls(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	lq := readSuListQuery(q)
	page, err := data.ListShortUrls(r.Context(), a.Db, suFiltersOf(lq, user))
	if err != nil {
		return err
	}
	ids := make([]core.ShortUrlID, len(page.Items))
	for i, d := range page.Items {
		ids[i] = d.Id
	}
	tagsByUrl, err := data.TagsForShortUrls(r.Context(), a.Db, ids)
	if err != nil {
		return err
	}
	table := a.suTable(lq, page, tagsByUrl)

	if isHtmx(r) {
		return a.renderShared(w, http.StatusOK, "su-table", table)
	}

	allTags, err := data.ListAllTagNames(r.Context(), a.Db)
	if err != nil {
		return err
	}
	filterGroups, err := a.groupChoices(r.Context(), user)
	if err != nil {
		return err
	}

	a.renderPage(w, http.StatusOK, "shorturls", user, "/admin/short-urls", "Short URLs", suListView{
		Search:       lq.Search,
		TagOptions:   optionsOf(allTags, lq.Tag),
		GroupOptions: optionsOf(filterGroups, core.NormalizeGroup(lq.Group)),
		Table:        table,
	})
	return nil
}

// ---- create ----

// suCreateForm is everything the create form posted, echoed back verbatim on
// errors so the visitor never loses their input.
type suCreateForm struct {
	LongUrl        string
	CustomSlug     string
	Domain         string
	Title          string
	Tags           string
	Group          string
	ValidSince     string
	ValidUntil     string
	MaxVisits      string
	RedirectStatus int
	ForwardQuery   bool
	Crawlable      bool
}

func emptySuCreateForm() suCreateForm {
	return suCreateForm{RedirectStatus: 302, ForwardQuery: true}
}

func readSuCreateForm(r *http.Request) suCreateForm {
	status, err := strconv.Atoi(r.PostFormValue("redirectStatus"))
	if err != nil {
		status = 302
	}
	return suCreateForm{
		LongUrl:        r.PostFormValue("longUrl"),
		CustomSlug:     r.PostFormValue("customSlug"),
		Domain:         r.PostFormValue("domain"),
		Title:          r.PostFormValue("title"),
		Tags:           r.PostFormValue("tags"),
		Group:          r.PostFormValue("group"),
		ValidSince:     r.PostFormValue("validSince"),
		ValidUntil:     r.PostFormValue("validUntil"),
		MaxVisits:      r.PostFormValue("maxVisits"),
		RedirectStatus: status,
		ForwardQuery:   r.PostFormValue("forwardQuery") == "true",
		Crawlable:      r.PostFormValue("crawlable") == "true",
	}
}

// groupChoices lists the groups a user may pick from: admins see every group
// referenced so far, others their own token groups.
func (a *App) groupChoices(ctx context.Context, user *CurrentUser) ([]string, error) {
	if user.IsAdmin() {
		return data.ListGroupNames(ctx, a.Db)
	}
	return user.Groups, nil
}

type suCreateView struct {
	Error         string
	Form          suCreateForm
	DefaultDomain string
	Group         groupPickerView
	StatusOptions []statusOptionView
}

func (a *App) renderCreateForm(w http.ResponseWriter, status int, user *CurrentUser, errorMessage string, form suCreateForm) error {
	return a.renderPage(w, status, "shorturl_new", user, "/admin/short-urls", "New short URL", suCreateView{
		Error:         errorMessage,
		Form:          form,
		DefaultDomain: a.Cfg.DefaultDomain.Value(),
		Group:         newGroupPicker(user, form.Group),
		StatusOptions: statusOptions(form.RedirectStatus),
	})
}

// GET /admin/short-urls/new
func (a *App) uiCreateShortUrlForm(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	return a.renderCreateForm(w, http.StatusOK, user, "", emptySuCreateForm())
}

func optionalStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func parseOptionalInt64(v string) *int64 {
	if v == "" {
		return nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return &n
	}
	return nil
}

func splitTagsField(csv string) []string {
	var tags []string
	for _, tag := range strings.Split(csv, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// POST /admin/short-urls/new
func (a *App) uiCreateShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	form := readSuCreateForm(r)

	status := form.RedirectStatus
	forwardQuery := form.ForwardQuery
	crawlable := form.Crawlable

	var validSince, validUntil *time.Time
	if form.ValidSince != "" {
		validSince = parseDateLocal(form.ValidSince)
	}
	if form.ValidUntil != "" {
		validUntil = parseDateLocal(form.ValidUntil)
	}

	spec, serr := core.NewShortUrlSpec(core.ShortUrlSpecInput{
		LongUrl:        form.LongUrl,
		CustomSlug:     optionalStr(form.CustomSlug),
		Domain:         optionalStr(form.Domain),
		Title:          optionalStr(form.Title),
		Tags:           splitTagsField(form.Tags),
		Group:          optionalStr(form.Group),
		ValidSince:     validSince,
		ValidUntil:     validUntil,
		MaxVisits:      parseOptionalInt64(form.MaxVisits),
		RedirectStatus: &status,
		ForwardQuery:   &forwardQuery,
		Crawlable:      &crawlable,
	})

	if serr == nil && !userMayAssignGroup(user, spec.Group) {
		serr = core.NewError(core.ErrInvalidGroup,
			"You can only assign groups you are a member of.")
	}
	if serr == nil {
		_, serr = a.CreateShortUrl(r.Context(), UserAuthor(user.Id), spec)
	}
	if serr == nil {
		return redirect(w, r, "/admin/short-urls")
	}
	if !isShortUrlUserError(serr) {
		return serr
	}
	return a.renderCreateForm(w, http.StatusBadRequest, user, serr.Error(), form)
}

// userMayAssignGroup: admins may assign any group; other users only groups
// they belong to (or no group).
func userMayAssignGroup(user *CurrentUser, group *core.GroupName) bool {
	if group == nil || user.IsAdmin() {
		return true
	}
	return core.GroupsContain(user.Groups, group.Value())
}

// ---- edit ----

func conditionLabel(c core.RuleCondition) string {
	switch c.Type {
	case core.CondDevice:
		return "Device is " + c.Value
	case core.CondLanguage:
		return "Language matches " + c.Value
	case core.CondQueryParam:
		return fmt.Sprintf("Query param %s = %s", c.Key, c.Value)
	case core.CondIPAddress:
		return "IP in " + c.Value
	default:
		return string(c.Type)
	}
}

type suEditView struct {
	Error              string
	ShortUrl           string
	QrUrl              string
	VisitsUrl          string
	VisitCount         int64
	EditAction         string
	LongUrl            string
	Title              string
	Tags               string
	Group              groupPickerView
	ValidSince         string
	ValidUntil         string
	MaxVisits          string
	StatusOptions      []statusOptionView
	ForwardQuery       bool
	Crawlable          bool
	Rules              []suRuleView
	RuleAddAction      string
	RuleDeleteAction   string
	DeleteAction       string
	VisitsDeleteAction string
}

type suRuleView struct {
	Priority   int
	Conditions []string
	LongUrl    string
}

// loadDetailFromPath resolves the {id} route parameter and enforces the
// user's group scope: a link outside the scope is indistinguishable from a
// missing one.
func (a *App) loadDetailFromPath(user *CurrentUser, r *http.Request) (*data.ShortUrlDetail, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, errPageNotFound
	}
	detail, err := data.ShortUrlDetailByID(r.Context(), a.Db, core.ShortUrlID(id))
	if err != nil {
		return nil, err
	}
	if detail == nil || !user.CanSeeGroup(detail.GroupName) {
		return nil, errPageNotFound
	}
	return detail, nil
}

func (a *App) respondEditPage(ctx context.Context, w http.ResponseWriter, status int, user *CurrentUser, detail *data.ShortUrlDetail, errorMessage string) error {
	tags, err := data.TagsForShortUrl(ctx, a.Db, detail.Id)
	if err != nil {
		return err
	}
	rules, err := data.RedirectRules(ctx, a.Db, detail.Id)
	if err != nil {
		return err
	}

	maxVisits := ""
	if detail.MaxVisits != nil {
		maxVisits = strconv.FormatInt(*detail.MaxVisits, 10)
	}
	model := suEditView{
		Error:              errorMessage,
		ShortUrl:           ShortUrlFor(a.Cfg, detail.Authority, detail.ShortCode),
		QrUrl:              "/" + detail.ShortCode + "/qr-code?size=300",
		VisitsUrl:          fmt.Sprintf("/admin/short-urls/%d/visits", detail.Id),
		VisitCount:         detail.VisitCount,
		EditAction:         fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id),
		LongUrl:            detail.LongUrl,
		Title:              valueOrEmpty(detail.Title),
		Tags:               strings.Join(tags, ", "),
		Group:              newGroupPicker(user, valueOrEmpty(detail.GroupName)),
		ValidSince:         dateLocalValue(detail.ValidSince),
		ValidUntil:         dateLocalValue(detail.ValidUntil),
		MaxVisits:          maxVisits,
		StatusOptions:      statusOptions(detail.RedirectStatus),
		ForwardQuery:       detail.ForwardQuery,
		Crawlable:          detail.Crawlable,
		RuleAddAction:      fmt.Sprintf("/admin/short-urls/%d/rules/add", detail.Id),
		RuleDeleteAction:   fmt.Sprintf("/admin/short-urls/%d/rules/delete", detail.Id),
		DeleteAction:       fmt.Sprintf("/admin/short-urls/%d/delete", detail.Id),
		VisitsDeleteAction: fmt.Sprintf("/admin/short-urls/%d/visits/delete", detail.Id),
	}
	for _, rule := range rules {
		rv := suRuleView{Priority: rule.Priority, LongUrl: rule.LongUrl}
		for _, c := range rule.Conditions {
			rv.Conditions = append(rv.Conditions, conditionLabel(c))
		}
		model.Rules = append(model.Rules, rv)
	}
	return a.renderPage(w, status, "shorturl_edit", user, "/admin/short-urls", "Edit short URL", model)
}

// GET /admin/short-urls/{id}/edit
func (a *App) uiEditShortUrlForm(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	return a.respondEditPage(r.Context(), w, http.StatusOK, user, detail, "")
}

// POST /admin/short-urls/{id}/edit
func (a *App) uiEditShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	get := r.PostFormValue

	status, err := strconv.Atoi(get("redirectStatus"))
	if err != nil {
		status = detail.RedirectStatus
	}

	var validSince, validUntil *time.Time
	if get("validSince") != "" {
		validSince = parseDateLocal(get("validSince"))
	}
	if get("validUntil") != "" {
		validUntil = parseDateLocal(get("validUntil"))
	}

	edit, serr := core.NewShortUrlEdit(core.ShortUrlEditInput{
		LongUrl:        get("longUrl"),
		Title:          optionalStr(get("title")),
		Group:          optionalStr(get("group")),
		ValidSince:     validSince,
		ValidUntil:     validUntil,
		MaxVisits:      parseOptionalInt64(get("maxVisits")),
		RedirectStatus: status,
		ForwardQuery:   get("forwardQuery") == "true",
		Crawlable:      get("crawlable") == "true",
		Tags:           splitTagsField(get("tags")),
		ChangeTags:     true,
	})
	if serr == nil && !userMayAssignGroup(user, edit.Group) {
		serr = core.NewError(core.ErrInvalidGroup,
			"You can only assign groups you are a member of.")
	}
	if serr != nil {
		return a.respondEditPage(r.Context(), w, http.StatusBadRequest, user, detail, serr.Error())
	}
	if _, err := a.EditShortUrl(r.Context(), detail.Id, detail, edit); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id))
}

// POST /admin/short-urls/{id}/rules/add
func (a *App) uiAddRule(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	get := r.PostFormValue

	var conditions []core.RuleCondition
	if device, ok := core.DeviceOfSlug(get("device")); ok {
		conditions = append(conditions, core.DeviceIs(device))
	}
	if lang := strings.TrimSpace(get("language")); lang != "" {
		conditions = append(conditions, core.LanguageIs(lang))
	}
	if key := strings.TrimSpace(get("queryKey")); key != "" {
		conditions = append(conditions, core.QueryParamIs(key, strings.TrimSpace(get("queryValue"))))
	}
	if ip := strings.TrimSpace(get("ipAddress")); ip != "" {
		conditions = append(conditions, core.IPInRange(ip))
	}

	target, err := core.NewLongUrl(get("ruleLongUrl"))
	if err != nil {
		return a.respondEditPage(r.Context(), w, http.StatusBadRequest, user, detail, "Could not add rule: "+err.Error())
	}
	if len(conditions) == 0 {
		return a.respondEditPage(r.Context(), w, http.StatusBadRequest, user, detail,
			"A rule needs at least one condition (device, language, query param or IP).")
	}

	rules, err := data.RedirectRules(r.Context(), a.Db, detail.Id)
	if err != nil {
		return err
	}
	newRule := core.RedirectRule{
		Priority:   len(rules) + 1,
		LongUrl:    target.Value(),
		Conditions: conditions,
	}
	if err := data.SetRedirectRules(r.Context(), a.Db, detail.Id, append(rules, newRule)); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id))
}

// POST /admin/short-urls/{id}/rules/delete
func (a *App) uiDeleteRule(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	if err := r.ParseForm(); err != nil {
		return BadRequest("Invalid form submission.")
	}
	priority, err := strconv.Atoi(r.PostFormValue("priority"))
	if err != nil {
		priority = -1
	}
	rules, err := data.RedirectRules(r.Context(), a.Db, detail.Id)
	if err != nil {
		return err
	}
	var remaining []core.RedirectRule
	for _, rule := range rules {
		if rule.Priority != priority {
			remaining = append(remaining, rule)
		}
	}
	if err := data.SetRedirectRules(r.Context(), a.Db, detail.Id, remaining); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id))
}

// POST /admin/short-urls/{id}/delete
func (a *App) uiDeleteShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	if _, err := data.DeleteShortUrl(r.Context(), a.Db, detail.Id); err != nil {
		return err
	}
	return redirect(w, r, "/admin/short-urls")
}

// POST /admin/short-urls/{id}/visits/delete
func (a *App) uiDeleteShortUrlVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) error {
	detail, err := a.loadDetailFromPath(user, r)
	if err != nil {
		return err
	}
	if _, err := data.DeleteVisitsForShortUrl(r.Context(), a.Db, detail.Id); err != nil {
		return err
	}
	return redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id))
}
