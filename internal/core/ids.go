package core

// Strongly-typed identifiers. Rows are scanned straight into these, so from
// the repository boundary up a domain id can never be passed where a
// short-url id is expected.

type ShortURLID int64

func (id ShortURLID) Value() int64 { return int64(id) }

type DomainID int64

func (id DomainID) Value() int64 { return int64(id) }

type VisitID int64

func (id VisitID) Value() int64 { return int64(id) }

type UserID int64

func (id UserID) Value() int64 { return int64(id) }

type APIKeyID int64

func (id APIKeyID) Value() int64 { return int64(id) }

type WebhookID int64

func (id WebhookID) Value() int64 { return int64(id) }
