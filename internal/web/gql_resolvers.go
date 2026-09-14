package web

// GraphQL adapters call the same operations as REST. Kept by gqlgen; new
// schema fields require an adapter here.

import (
	"context"
	"strconv"
)

type GraphResolver struct{ App *App }

// CreateShortURL is the resolver for the createShortURL field.
func (r *mutationGraphResolver) CreateShortURL(ctx context.Context, input CreateShortURLBody) (*ShortURLDTO, error) {
	return r.App.opCreateShortURL(ctx, keyFromContext(ctx), &BodyInput[CreateShortURLBody]{Body: input})
}

// UpdateShortURL is the resolver for the updateShortURL field.
func (r *mutationGraphResolver) UpdateShortURL(ctx context.Context, code string, domain *string, input UpdateShortURLInput) (*ShortURLDTO, error) {
	body := EditShortURLBody{LongURL: patchField(input.LongURL), Title: patchField(input.Title), Group: patchField(input.Group), MaxVisits: patchField(input.MaxVisits), ValidSince: patchField(input.ValidSince), ValidUntil: patchField(input.ValidUntil), ForwardQuery: patchField(input.ForwardQuery), Crawlable: patchField(input.Crawlable), RedirectStatus: patchField(input.RedirectStatus), Tags: Field[[]string]{Present: input.Tags.IsSet(), Null: input.Tags.Value() == nil, Value: input.Tags.Value()}}
	return r.App.opEditShortURL(ctx, keyFromContext(ctx), &ShortURLBodyInput[EditShortURLBody]{ShortURLInput: linkInput(code, domain), Body: body})
}

// DeleteShortURL is the resolver for the deleteShortURL field.
func (r *mutationGraphResolver) DeleteShortURL(ctx context.Context, code string, domain *string) (bool, error) {
	_, err := r.App.opDeleteShortURL(ctx, keyFromContext(ctx), ptr(linkInput(code, domain)))
	return err == nil, err
}

// SetRedirectRules is the resolver for the setRedirectRules field.
func (r *mutationGraphResolver) SetRedirectRules(ctx context.Context, code string, domain *string, rules []RuleBody) (*rulesDTO, error) {
	return r.App.opSetRules(ctx, keyFromContext(ctx), &ShortURLBodyInput[SetRulesBody]{ShortURLInput: linkInput(code, domain), Body: SetRulesBody{RedirectRules: rules}})
}

// DeleteShortURLVisits is the resolver for the deleteShortURLVisits field.
func (r *mutationGraphResolver) DeleteShortURLVisits(ctx context.Context, code string, domain *string) (int64, error) {
	out, err := r.App.opDeleteShortURLVisits(ctx, keyFromContext(ctx), ptr(linkInput(code, domain)))
	if err != nil {
		return 0, err
	}
	return int64(out.DeletedVisits), nil
}

// RenameTag is the resolver for the renameTag field.
func (r *mutationGraphResolver) RenameTag(ctx context.Context, oldName string, newName string) (*RenameTagBody, error) {
	return r.App.opRenameTag(ctx, keyFromContext(ctx), &BodyInput[RenameTagBody]{Body: RenameTagBody{OldName: oldName, NewName: newName}})
}

// DeleteTags is the resolver for the deleteTags field.
func (r *mutationGraphResolver) DeleteTags(ctx context.Context, tags []string) (int64, error) {
	out, err := r.App.opDeleteTags(ctx, keyFromContext(ctx), &DeleteTagsInput{Tags: tags})
	if err != nil {
		return 0, err
	}
	return int64(out.DeletedTags), nil
}

// CreateDomain is the resolver for the createDomain field.
func (r *mutationGraphResolver) CreateDomain(ctx context.Context, domain string) (*domainDTO, error) {
	return r.App.opCreateDomain(ctx, keyFromContext(ctx), &BodyInput[CreateDomainBody]{Body: CreateDomainBody{Domain: domain}})
}

// UpdateDomainRedirects is the resolver for the updateDomainRedirects field.
func (r *mutationGraphResolver) UpdateDomainRedirects(ctx context.Context, input DomainRedirectsBody) (*domainDTO, error) {
	return r.App.opSetDomainRedirects(ctx, keyFromContext(ctx), &BodyInput[DomainRedirectsBody]{Body: input})
}

// DeleteDomain is the resolver for the deleteDomain field.
func (r *mutationGraphResolver) DeleteDomain(ctx context.Context, authority string) (bool, error) {
	_, err := r.App.opDeleteDomain(ctx, keyFromContext(ctx), &AuthorityInput{Authority: authority})
	return err == nil, err
}

// DeleteOrphanVisits is the resolver for the deleteOrphanVisits field.
func (r *mutationGraphResolver) DeleteOrphanVisits(ctx context.Context) (int64, error) {
	out, err := r.App.opDeleteOrphanVisits(ctx, keyFromContext(ctx), &Empty{})
	if err != nil {
		return 0, err
	}
	return int64(out.DeletedVisits), nil
}

// CreateAPIKey is the resolver for the createAPIKey field.
func (r *mutationGraphResolver) CreateAPIKey(ctx context.Context, input CreateAPIKeyBody) (*apiKeyDTO, error) {
	return r.App.opCreateAPIKey(ctx, keyFromContext(ctx), &BodyInput[CreateAPIKeyBody]{Body: input})
}

// SetAPIKeyEnabled is the resolver for the setAPIKeyEnabled field.
func (r *mutationGraphResolver) SetAPIKeyEnabled(ctx context.Context, id int64, enabled bool) (*IDEnabled, error) {
	return r.App.opPatchAPIKey(ctx, keyFromContext(ctx), &IDBodyInput[PatchAPIKeyBody]{IDInput: IDInput{ID: strconv.FormatInt(id, 10)}, Body: PatchAPIKeyBody{Enabled: enabled}})
}

// DeleteAPIKey is the resolver for the deleteAPIKey field.
func (r *mutationGraphResolver) DeleteAPIKey(ctx context.Context, id int64) (bool, error) {
	_, err := r.App.opDeleteAPIKey(ctx, keyFromContext(ctx), &IDInput{ID: strconv.FormatInt(id, 10)})
	return err == nil, err
}

// CreateWebhook is the resolver for the createWebhook field.
func (r *mutationGraphResolver) CreateWebhook(ctx context.Context, input CreateWebhookBody) (*webhookDTO, error) {
	return r.App.opCreateWebhook(ctx, keyFromContext(ctx), &BodyInput[CreateWebhookBody]{Body: input})
}

// SetWebhookEnabled is the resolver for the setWebhookEnabled field.
func (r *mutationGraphResolver) SetWebhookEnabled(ctx context.Context, id int64, enabled bool) (*IDEnabled, error) {
	return r.App.opPatchWebhook(ctx, keyFromContext(ctx), &IDBodyInput[PatchWebhookBody]{IDInput: IDInput{ID: strconv.FormatInt(id, 10)}, Body: PatchWebhookBody{Enabled: enabled}})
}

// DeleteWebhook is the resolver for the deleteWebhook field.
func (r *mutationGraphResolver) DeleteWebhook(ctx context.Context, id int64) (bool, error) {
	_, err := r.App.opDeleteWebhook(ctx, keyFromContext(ctx), &IDInput{ID: strconv.FormatInt(id, 10)})
	return err == nil, err
}

// ShortURLs is the resolver for the shortURLs field.
func (r *queryGraphResolver) ShortURLs(ctx context.Context, filter *ShortURLFilter) (*PageDTO[ShortURLDTO], error) {
	return r.App.opListShortURLs(ctx, keyFromContext(ctx), shortURLFilterInput(filter))
}

// ShortURL is the resolver for the shortURL field.
func (r *queryGraphResolver) ShortURL(ctx context.Context, code string, domain *string) (*ShortURLDTO, error) {
	return r.App.opGetShortURL(ctx, keyFromContext(ctx), ptr(linkInput(code, domain)))
}

// Tags is the resolver for the tags field.
func (r *queryGraphResolver) Tags(ctx context.Context, searchTerm *string, page *int, itemsPerPage *int) (*PageDTO[tagStatsDTO], error) {
	return r.App.opTagStats(ctx, keyFromContext(ctx), &TagListInput{PageQuery: pageQuery(page, itemsPerPage), SearchTerm: value(searchTerm), WithStats: "true"})
}

// Domains is the resolver for the domains field.
func (r *queryGraphResolver) Domains(ctx context.Context) ([]domainDTO, error) {
	out, err := r.App.opListDomains(ctx, keyFromContext(ctx), &Empty{})
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// VisitsOverview is the resolver for the visitsOverview field.
func (r *queryGraphResolver) VisitsOverview(ctx context.Context) (*VisitOverviewDTO, error) {
	return r.App.opVisitsOverview(ctx, keyFromContext(ctx), &Empty{})
}

// Visits is the resolver for the visits field.
func (r *queryGraphResolver) Visits(ctx context.Context, filter *VisitFilter) (*PageDTO[VisitDTO], error) {
	return r.App.opListNonOrphanVisits(ctx, keyFromContext(ctx), ptr(visitQuery(filter)))
}

// OrphanVisits is the resolver for the orphanVisits field.
func (r *queryGraphResolver) OrphanVisits(ctx context.Context, typeArg *string, filter *VisitFilter) (*PageDTO[VisitDTO], error) {
	return r.App.opListOrphanVisits(ctx, keyFromContext(ctx), &OrphanVisitsInput{VisitQuery: visitQuery(filter), Type: value(typeArg)})
}

// TagVisits is the resolver for the tagVisits field.
func (r *queryGraphResolver) TagVisits(ctx context.Context, tag string, filter *VisitFilter) (*PageDTO[VisitDTO], error) {
	return r.App.opTagVisits(ctx, keyFromContext(ctx), &TagVisitsInput{Tag: tag, VisitQuery: visitQuery(filter)})
}

// DomainVisits is the resolver for the domainVisits field.
func (r *queryGraphResolver) DomainVisits(ctx context.Context, authority string, filter *VisitFilter) (*PageDTO[VisitDTO], error) {
	return r.App.opDomainVisits(ctx, keyFromContext(ctx), &DomainVisitsInput{AuthorityInput: AuthorityInput{Authority: authority}, VisitQuery: visitQuery(filter)})
}

// VisitsPerDay is the resolver for the visitsPerDay field.
func (r *queryGraphResolver) VisitsPerDay(ctx context.Context, scope *StatsScope) ([]dayDTO, error) {
	out, err := r.App.opVisitsPerDay(ctx, keyFromContext(ctx), ptr(statsInput(scope)))
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Breakdown is the resolver for the breakdown field.
func (r *queryGraphResolver) Breakdown(ctx context.Context, by string, scope *StatsScope, limit *int) ([]breakdownDTO, error) {
	out, err := r.App.opBreakdown(ctx, keyFromContext(ctx), &BreakdownInput{StatsInput: statsInput(scope), By: by, Limit: intQuery(limit)})
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// APIKeys is the resolver for the apiKeys field.
func (r *queryGraphResolver) APIKeys(ctx context.Context) ([]apiKeyDTO, error) {
	out, err := r.App.opListAPIKeys(ctx, keyFromContext(ctx), &Empty{})
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Webhooks is the resolver for the webhooks field.
func (r *queryGraphResolver) Webhooks(ctx context.Context) ([]webhookDTO, error) {
	out, err := r.App.opListWebhooks(ctx, keyFromContext(ctx), &Empty{})
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Visits is the resolver for the visits field.
func (r *shortURLGraphResolver) Visits(ctx context.Context, obj *ShortURLDTO, filter *VisitFilter) (*PageDTO[VisitDTO], error) {
	return r.App.opListShortURLVisits(ctx, keyFromContext(ctx), &ShortURLVisitsInput{ShortURLInput: ShortURLInput{Code: obj.ShortCode, Domain: obj.Domain}, VisitQuery: visitQuery(filter)})
}

// RedirectRules is the resolver for the redirectRules field.
func (r *shortURLGraphResolver) RedirectRules(ctx context.Context, obj *ShortURLDTO) (*rulesDTO, error) {
	return r.App.opGetRules(ctx, keyFromContext(ctx), &ShortURLInput{Code: obj.ShortCode, Domain: obj.Domain})
}

// Mutation returns MutationResolver implementation.
func (r *GraphResolver) Mutation() MutationResolver { return &mutationGraphResolver{r} }

// Query returns QueryResolver implementation.
func (r *GraphResolver) Query() QueryResolver { return &queryGraphResolver{r} }

// ShortURL returns ShortURLResolver implementation.
func (r *GraphResolver) ShortURL() ShortURLResolver { return &shortURLGraphResolver{r} }

type (
	mutationGraphResolver struct{ *GraphResolver }
	queryGraphResolver    struct{ *GraphResolver }
	shortURLGraphResolver struct{ *GraphResolver }
)

// !!! WARNING !!!
// The code below was going to be deleted when updating resolvers. It has been copied here so you have
// one last chance to move it out of harms way if you want. There are two reasons this happens:
//  - When renaming or deleting a resolver the old code will be put in here. You can safely delete
//    it when you're done.
//  - You have helper methods in this file. Move them out to keep these resolver files clean.
/*
	type GraphResolver struct{ App *App }
*/
