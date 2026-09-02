package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olmesm/gort/internal/core"
	"github.com/olmesm/gort/internal/data"
	"github.com/olmesm/gort/internal/h"
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

func sortHeader(lq suListQuery, field, label string) h.Node {
	nextDir := "desc"
	if lq.OrderBy == field && lq.Dir == "desc" {
		nextDir = "asc"
	}
	next := lq
	next.OrderBy, next.Dir, next.Page = field, nextDir, 1
	sortUrl := suListUrl(next, 1)

	marker := ""
	if lq.OrderBy == field {
		if lq.Dir == "asc" {
			marker = " ↑"
		} else {
			marker = " ↓"
		}
	}
	return h.E("th", nil,
		h.E("a", []h.Attr{
			h.A("href", sortUrl),
			hxGet(sortUrl),
			hxTarget("#su-table"),
			hxSwap("outerHTML"),
			hxPushUrl(),
		}, h.Text(label+marker)))
}

func (a *App) suTable(lq suListQuery, page core.Page[data.ShortUrlDetail], tagsByUrl map[int64][]string) h.Node {
	var rows []h.Node
	for _, d := range page.Items {
		shortUrl := ShortUrlFor(a.Cfg, d.Authority, d.ShortCode)
		title := "—"
		if d.Title != nil {
			title = *d.Title
		}
		var badges []h.Node
		for _, tag := range tagsByUrl[d.Id] {
			badges = append(badges, h.E("span", []h.Attr{h.A("class", "badge")}, h.Text(tag)))
		}
		groupCell := h.Node(h.Text("—"))
		if d.GroupName != nil {
			groupCell = h.E("span", []h.Attr{h.A("class", "badge gray")}, h.Text(*d.GroupName))
		}
		rows = append(rows, h.E("tr", nil,
			h.E("td", nil,
				h.E("a", []h.Attr{
					h.A("class", "mono"), h.A("href", shortUrl),
					h.A("target", "_blank"), h.A("rel", "noreferrer"),
				}, h.Text(d.Authority+"/"+d.ShortCode))),
			h.E("td", nil,
				h.E("span", []h.Attr{h.A("class", "truncate"), h.A("style", "max-width:200px")}, h.Text(title))),
			h.E("td", nil,
				h.E("a", []h.Attr{
					h.A("class", "truncate"), h.A("href", d.LongUrl),
					h.A("target", "_blank"), h.A("rel", "noreferrer"),
				}, h.Text(d.LongUrl))),
			h.E("td", nil, badges...),
			h.E("td", nil, groupCell),
			h.E("td", nil,
				h.E("a", []h.Attr{h.A("href", fmt.Sprintf("/admin/short-urls/%d/visits", d.Id))},
					h.Text(formatCount(d.VisitCount)))),
			h.E("td", []h.Attr{h.A("class", "muted")}, h.Text(formatDateTime(d.CreatedAt))),
			h.E("td", []h.Attr{h.A("class", "actions")},
				h.E("a", []h.Attr{
					h.A("class", "btn secondary small"),
					h.A("href", fmt.Sprintf("/admin/short-urls/%d/edit", d.Id)),
				}, h.Text("Edit")))))
	}

	return h.E("div", []h.Attr{h.A("id", "su-table")},
		h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						sortHeader(lq, "shortCode", "Short URL"),
						sortHeader(lq, "title", "Title"),
						sortHeader(lq, "longUrl", "Long URL"),
						h.E("th", nil, h.Text("Tags")),
						h.E("th", nil, h.Text("Group")),
						sortHeader(lq, "visits", "Visits"),
						sortHeader(lq, "dateCreated", "Created"),
						h.E("th", nil))),
				h.E("tbody", nil, rows...))),
		pager(func(p int) string { return suListUrl(lq, p) }, page))
}

// GET /admin/short-urls (full page or htmx fragment)
func (a *App) uiListShortUrls(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lq := readSuListQuery(q)
	page, err := data.ListShortUrls(a.Db, suFiltersOf(lq, user))
	if err != nil {
		a.serverError(w, err)
		return
	}
	ids := make([]core.ShortUrlID, len(page.Items))
	for i, d := range page.Items {
		ids[i] = core.ShortUrlID(d.Id)
	}
	tagsByUrl, err := data.TagsForShortUrls(a.Db, ids)
	if err != nil {
		a.serverError(w, err)
		return
	}
	table := a.suTable(lq, page, tagsByUrl)

	if isHtmx(r) {
		respondFragment(w, table)
		return
	}

	allTags, err := data.ListAllTagNames(a.Db)
	if err != nil {
		a.serverError(w, err)
		return
	}
	tagOptions := []h.Node{h.E("option", []h.Attr{h.A("value", "")}, h.Text("All tags"))}
	for _, tag := range allTags {
		tagOptions = append(tagOptions, h.E("option", []h.Attr{
			h.A("value", tag),
			h.If(tag == lq.Tag, h.Flag("selected")),
		}, h.Text(tag)))
	}

	filterGroups, err := a.groupChoices(user)
	if err != nil {
		a.serverError(w, err)
		return
	}
	groupOptions := []h.Node{h.E("option", []h.Attr{h.A("value", "")}, h.Text("All groups"))}
	for _, group := range filterGroups {
		groupOptions = append(groupOptions, h.E("option", []h.Attr{
			h.A("value", group),
			h.If(group == core.NormalizeGroup(lq.Group), h.Flag("selected")),
		}, h.Text(group)))
	}

	content := []h.Node{
		h.E("h1", nil, h.Text("Short URLs")),
		h.E("div", []h.Attr{h.A("class", "toolbar")},
			h.E("form", []h.Attr{
				hxGet("/admin/short-urls"),
				hxTarget("#su-table"),
				hxSwap("outerHTML"),
				hxTrigger("submit, input delay:400ms from:input[name='search'], change from:select"),
				hxPushUrl(),
				h.A("method", "get"),
				h.A("action", "/admin/short-urls"),
			},
				h.E("input", []h.Attr{
					h.A("type", "search"), h.A("name", "search"), h.A("value", lq.Search),
					h.A("placeholder", "Search code, URL, title or tag…"),
				}),
				h.E("select", []h.Attr{h.A("name", "tag")}, tagOptions...),
				h.E("select", []h.Attr{h.A("name", "group")}, groupOptions...),
				h.E("button", []h.Attr{h.A("class", "secondary")}, h.Text("Filter"))),
			h.E("a", []h.Attr{h.A("class", "btn"), h.A("href", "/admin/short-urls/new")},
				h.Text("+ New short URL"))),
		table,
	}
	respondPage(w, user, "/admin/short-urls", "Short URLs", content)
}

// ---- create ----

func redirectStatusSelect(current int) h.Node {
	options := []struct {
		status int
		label  string
	}{
		{301, "301 — permanent"},
		{302, "302 — found (default)"},
		{307, "307 — temporary, keep method"},
		{308, "308 — permanent, keep method"},
	}
	var nodes []h.Node
	for _, opt := range options {
		nodes = append(nodes, h.E("option", []h.Attr{
			h.A("value", strconv.Itoa(opt.status)),
			h.If(opt.status == current, h.Flag("selected")),
		}, h.Text(opt.label)))
	}
	return h.E("select", []h.Attr{h.A("name", "redirectStatus")}, nodes...)
}

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
func (a *App) groupChoices(user *CurrentUser) ([]string, error) {
	if user.IsAdmin() {
		return data.ListGroupNames(a.Db)
	}
	return user.Groups, nil
}

// groupInput renders the group picker: a free text input for admins (any
// group, existing or new), a fixed choice of the user's own groups otherwise.
func groupInput(user *CurrentUser, current string) h.Node {
	if user.IsAdmin() {
		return textInput("group", current, "team-a (optional)")
	}
	options := []h.Node{h.E("option", []h.Attr{h.A("value", "")}, h.Text("No group"))}
	for _, group := range user.Groups {
		options = append(options, h.E("option", []h.Attr{
			h.A("value", group),
			h.If(group == current, h.Flag("selected")),
		}, h.Text(group)))
	}
	return h.E("select", []h.Attr{h.A("name", "group")}, options...)
}

func (a *App) suCreateFormContent(user *CurrentUser, errorMessage string, form suCreateForm) []h.Node {
	errorNode := h.Empty()
	if errorMessage != "" {
		errorNode = alertError(errorMessage)
	}
	return []h.Node{
		h.E("h1", nil, h.Text("New short URL")),
		errorNode,
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{h.A("class", "stack"), h.A("method", "post"), h.A("action", "/admin/short-urls/new")},
				formField("Long URL *",
					h.E("input", []h.Attr{
						h.A("type", "url"), h.A("name", "longUrl"), h.A("value", form.LongUrl), h.Flag("required"),
						h.A("placeholder", "https://example.com/some/very/long/path"),
					})),
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Custom slug (optional)", textInput("customSlug", form.CustomSlug, "my-campaign")),
					formField("Domain (optional)", textInput("domain", form.Domain, a.Cfg.DefaultDomain.Value()))),
				formField("Title (optional; auto-resolved when empty)", textInput("title", form.Title, "")),
				formField("Tags (comma separated)", textInput("tags", form.Tags, "marketing, launch")),
				formField("Group (limits visibility to its members)", groupInput(user, form.Group)),
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Valid since (UTC)",
						h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "validSince"), h.A("value", form.ValidSince)})),
					formField("Valid until (UTC)",
						h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "validUntil"), h.A("value", form.ValidUntil)})),
					formField("Max visits",
						h.E("input", []h.Attr{h.A("type", "number"), h.A("name", "maxVisits"), h.A("value", form.MaxVisits), h.A("min", "1")}))),
				formField("Redirect status", redirectStatusSelect(form.RedirectStatus)),
				checkbox("forwardQuery", form.ForwardQuery, "Forward query params to the long URL"),
				checkbox("crawlable", form.Crawlable, "Allow search engines to crawl this short URL"),
				h.E("div", nil, h.E("button", nil, h.Text("Create short URL"))))),
	}
}

// GET /admin/short-urls/new
func (a *App) uiCreateShortUrlForm(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	respondPage(w, user, "/admin/short-urls", "New short URL",
		a.suCreateFormContent(user, "", emptySuCreateForm()))
}

func valueOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
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
func (a *App) uiCreateShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
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
		serr = core.NewShortUrlError(core.ErrInvalidGroup,
			"You can only assign groups you are a member of.")
	}
	if serr == nil {
		_, serr = a.CreateShortUrl(UserAuthor(user.Id), spec)
	}
	if serr == nil {
		http.Redirect(w, r, "/admin/short-urls", http.StatusFound)
		return
	}
	respondHtml(w, http.StatusBadRequest,
		layoutPage(user, "/admin/short-urls", "New short URL",
			a.suCreateFormContent(user, serr.Message(), form)))
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

func (a *App) suEditPage(user *CurrentUser, detail *data.ShortUrlDetail, tags []string, rules []core.RedirectRule, banner h.Node) []h.Node {
	shortUrl := ShortUrlFor(a.Cfg, detail.Authority, detail.ShortCode)
	title := ""
	if detail.Title != nil {
		title = *detail.Title
	}
	maxVisits := ""
	if detail.MaxVisits != nil {
		maxVisits = strconv.FormatInt(*detail.MaxVisits, 10)
	}

	rulesSection := h.E("p", []h.Attr{h.A("class", "muted")}, h.Text("No rules configured."))
	if len(rules) > 0 {
		var ruleRows []h.Node
		for _, rule := range rules {
			var conditionItems []h.Node
			for _, c := range rule.Conditions {
				conditionItems = append(conditionItems, h.E("li", nil, h.Text(conditionLabel(c))))
			}
			ruleRows = append(ruleRows, h.E("tr", nil,
				h.E("td", nil, h.Text(strconv.Itoa(rule.Priority))),
				h.E("td", nil, h.E("ul", []h.Attr{h.A("class", "rule-conditions")}, conditionItems...)),
				h.E("td", nil, h.E("span", []h.Attr{h.A("class", "truncate")}, h.Text(rule.LongUrl))),
				h.E("td", []h.Attr{h.A("class", "actions")},
					h.E("form", []h.Attr{
						h.A("class", "inline"), h.A("method", "post"),
						h.A("action", fmt.Sprintf("/admin/short-urls/%d/rules/delete", detail.Id)),
					},
						h.E("input", []h.Attr{h.A("type", "hidden"), h.A("name", "priority"), h.A("value", strconv.Itoa(rule.Priority))}),
						h.E("button", []h.Attr{h.A("class", "danger small")}, h.Text("Remove"))))))
		}
		rulesSection = h.E("div", []h.Attr{h.A("class", "table-wrap")},
			h.E("table", nil,
				h.E("thead", nil,
					h.E("tr", nil,
						h.E("th", nil, h.Text("#")),
						h.E("th", nil, h.Text("Conditions")),
						h.E("th", nil, h.Text("Target URL")),
						h.E("th", nil))),
				h.E("tbody", nil, ruleRows...)))
	}

	return []h.Node{
		h.E("h1", nil, h.Text("Edit short URL")),
		banner,
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("p", nil,
				h.E("a", []h.Attr{h.A("class", "mono"), h.A("href", shortUrl), h.A("target", "_blank"), h.A("rel", "noreferrer")},
					h.Text(shortUrl)),
				h.Text(" · "),
				h.E("a", []h.Attr{h.A("href", "/"+detail.ShortCode+"/qr-code?size=300"), h.A("target", "_blank")},
					h.Text("QR code")),
				h.Text(" · "),
				h.E("a", []h.Attr{h.A("href", fmt.Sprintf("/admin/short-urls/%d/visits", detail.Id))},
					h.Text(fmt.Sprintf("%d visits", detail.VisitCount)))),
			h.E("form", []h.Attr{
				h.A("class", "stack"), h.A("method", "post"),
				h.A("action", fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id)),
			},
				formField("Long URL *",
					h.E("input", []h.Attr{h.A("type", "url"), h.A("name", "longUrl"), h.A("value", detail.LongUrl), h.Flag("required")})),
				formField("Title", textInput("title", title, "")),
				formField("Tags (comma separated)", textInput("tags", strings.Join(tags, ", "), "")),
				formField("Group (limits visibility to its members)", groupInput(user, valueOrEmpty(detail.GroupName))),
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Valid since (UTC)",
						h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "validSince"), h.A("value", dateLocalValue(detail.ValidSince))})),
					formField("Valid until (UTC)",
						h.E("input", []h.Attr{h.A("type", "datetime-local"), h.A("name", "validUntil"), h.A("value", dateLocalValue(detail.ValidUntil))})),
					formField("Max visits",
						h.E("input", []h.Attr{h.A("type", "number"), h.A("name", "maxVisits"), h.A("value", maxVisits), h.A("min", "1")}))),
				formField("Redirect status", redirectStatusSelect(detail.RedirectStatus)),
				checkbox("forwardQuery", detail.ForwardQuery, "Forward query params to the long URL"),
				checkbox("crawlable", detail.Crawlable, "Allow search engines to crawl this short URL"),
				h.E("div", nil, h.E("button", nil, h.Text("Save changes"))))),
		h.E("h2", nil, h.Text("Conditional redirect rules")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("p", []h.Attr{h.A("class", "muted")},
				h.Text("Rules are evaluated top-down; the first rule whose conditions all match overrides the long URL.")),
			rulesSection,
			h.E("h2", nil, h.Text("Add rule")),
			h.E("form", []h.Attr{
				h.A("class", "stack"), h.A("method", "post"),
				h.A("action", fmt.Sprintf("/admin/short-urls/%d/rules/add", detail.Id)),
			},
				formField("Target long URL *",
					h.E("input", []h.Attr{h.A("type", "url"), h.A("name", "ruleLongUrl"), h.Flag("required")})),
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Device",
						h.E("select", []h.Attr{h.A("name", "device")},
							h.E("option", []h.Attr{h.A("value", "")}, h.Text("Any device")),
							h.E("option", []h.Attr{h.A("value", "android")}, h.Text("Android")),
							h.E("option", []h.Attr{h.A("value", "ios")}, h.Text("iOS")),
							h.E("option", []h.Attr{h.A("value", "mobile")}, h.Text("Any mobile")),
							h.E("option", []h.Attr{h.A("value", "desktop")}, h.Text("Desktop")))),
					formField("Language (e.g. en or en-GB)", textInput("language", "", "")),
					formField("IP address / CIDR", textInput("ipAddress", "", "10.0.0.0/8"))),
				h.E("div", []h.Attr{h.A("class", "row")},
					formField("Query param name", textInput("queryKey", "", "utm_source")),
					formField("Query param value", textInput("queryValue", "", "newsletter"))),
				h.E("div", nil, h.E("button", []h.Attr{h.A("class", "secondary")}, h.Text("Add rule"))))),
		h.E("h2", nil, h.Text("Danger zone")),
		h.E("div", []h.Attr{h.A("class", "card")},
			h.E("form", []h.Attr{
				h.A("class", "inline"), h.A("method", "post"),
				h.A("action", fmt.Sprintf("/admin/short-urls/%d/delete", detail.Id)),
				h.A("onsubmit", "return confirm('Delete this short URL and all its visits?')"),
			},
				h.E("button", []h.Attr{h.A("class", "danger")}, h.Text("Delete short URL"))),
			h.Text(" "),
			h.E("form", []h.Attr{
				h.A("class", "inline"), h.A("method", "post"),
				h.A("action", fmt.Sprintf("/admin/short-urls/%d/visits/delete", detail.Id)),
				h.A("onsubmit", "return confirm('Delete all visits of this short URL?')"),
			},
				h.E("button", []h.Attr{h.A("class", "danger")}, h.Text("Delete its visits")))),
	}
}

// loadDetailFromPath resolves the {id} route parameter and enforces the
// user's group scope: a link outside the scope is indistinguishable from a
// missing one.
func (a *App) loadDetailFromPath(user *CurrentUser, w http.ResponseWriter, r *http.Request) *data.ShortUrlDetail {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondPlainNotFound(w)
		return nil
	}
	detail, err := data.TryGetDetailById(a.Db, core.ShortUrlID(id))
	if err != nil {
		a.serverError(w, err)
		return nil
	}
	if detail == nil || !user.CanSeeGroup(detail.GroupName) {
		respondPlainNotFound(w)
		return nil
	}
	return detail
}

func respondPlainNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("Not found"))
}

func (a *App) respondEditPage(w http.ResponseWriter, status int, user *CurrentUser, detail *data.ShortUrlDetail, banner h.Node) {
	tags, err := data.TagsForShortUrl(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		a.serverError(w, err)
		return
	}
	rules, err := data.GetRules(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		a.serverError(w, err)
		return
	}
	respondHtml(w, status,
		layoutPage(user, "/admin/short-urls", "Edit short URL", a.suEditPage(user, detail, tags, rules, banner)))
}

// GET /admin/short-urls/{id}/edit
func (a *App) uiEditShortUrlForm(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	a.respondEditPage(w, http.StatusOK, user, detail, h.Empty())
}

// POST /admin/short-urls/{id}/edit
func (a *App) uiEditShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
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
		serr = core.NewShortUrlError(core.ErrInvalidGroup,
			"You can only assign groups you are a member of.")
	}
	if serr != nil {
		a.respondEditPage(w, http.StatusBadRequest, user, detail, alertError(serr.Message()))
		return
	}
	if _, err := a.EditShortUrl(core.ShortUrlID(detail.Id), detail, edit); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id), http.StatusFound)
}

// POST /admin/short-urls/{id}/rules/add
func (a *App) uiAddRule(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
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
		a.respondEditPage(w, http.StatusBadRequest, user, detail,
			alertError("Could not add rule: "+err.Error()))
		return
	}
	if len(conditions) == 0 {
		a.respondEditPage(w, http.StatusBadRequest, user, detail,
			alertError("A rule needs at least one condition (device, language, query param or IP)."))
		return
	}

	rules, err := data.GetRules(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		a.serverError(w, err)
		return
	}
	newRule := core.RedirectRule{
		Priority:   len(rules) + 1,
		LongUrl:    target.Value(),
		Conditions: conditions,
	}
	if err := data.SetRules(a.Db, core.ShortUrlID(detail.Id), append(rules, newRule)); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id), http.StatusFound)
}

// POST /admin/short-urls/{id}/rules/delete
func (a *App) uiDeleteRule(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		BadRequest(w, "Invalid form submission.")
		return
	}
	priority, err := strconv.Atoi(r.PostFormValue("priority"))
	if err != nil {
		priority = -1
	}
	rules, err := data.GetRules(a.Db, core.ShortUrlID(detail.Id))
	if err != nil {
		a.serverError(w, err)
		return
	}
	var remaining []core.RedirectRule
	for _, rule := range rules {
		if rule.Priority != priority {
			remaining = append(remaining, rule)
		}
	}
	if err := data.SetRules(a.Db, core.ShortUrlID(detail.Id), remaining); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id), http.StatusFound)
}

// POST /admin/short-urls/{id}/delete
func (a *App) uiDeleteShortUrl(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	if _, err := data.DeleteShortUrl(a.Db, core.ShortUrlID(detail.Id)); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin/short-urls", http.StatusFound)
}

// POST /admin/short-urls/{id}/visits/delete
func (a *App) uiDeleteShortUrlVisits(user *CurrentUser, w http.ResponseWriter, r *http.Request) {
	detail := a.loadDetailFromPath(user, w, r)
	if detail == nil {
		return
	}
	if _, err := data.DeleteVisitsForShortUrl(a.Db, core.ShortUrlID(detail.Id)); err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/short-urls/%d/edit", detail.Id), http.StatusFound)
}
