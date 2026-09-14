package web

// Named aliases let gqlgen reuse the REST response models.
type GraphShortURLPage = PageDTO[ShortURLDTO]
type GraphVisitPage = PageDTO[VisitDTO]
type GraphTagPage = PageDTO[tagStatsDTO]
type GraphTag = tagStatsDTO
type GraphRules = rulesDTO
type GraphRule = ruleItemDTO
type GraphDomain = domainDTO
type GraphDomainRedirects = domainRedirectsDTO
type GraphAPIKey = apiKeyDTO
type GraphWebhook = webhookDTO
type GraphDay = dayDTO
type GraphBreakdown = breakdownDTO
