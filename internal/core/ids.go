package core

// Strongly-typed identifiers. Persistence rows carry raw int64s; everything
// above the row level speaks in these, so a domain id can never be passed
// where a short-url id is expected.

type ShortUrlID int64

func (id ShortUrlID) Value() int64 { return int64(id) }

type DomainID int64

func (id DomainID) Value() int64 { return int64(id) }

type VisitID int64

func (id VisitID) Value() int64 { return int64(id) }

type UserID int64

func (id UserID) Value() int64 { return int64(id) }

type ApiKeyID int64

func (id ApiKeyID) Value() int64 { return int64(id) }

type WebhookID int64

func (id WebhookID) Value() int64 { return int64(id) }
