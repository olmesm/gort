package core

// Shared pagination primitives used by list endpoints and UI tables.

type Page[T any] struct {
	Items        []T
	CurrentPage  int
	ItemsPerPage int
	TotalItems   int64
}

func (p Page[T]) TotalPages() int {
	if p.ItemsPerPage <= 0 {
		return 1
	}
	return int((p.TotalItems + int64(p.ItemsPerPage) - 1) / int64(p.ItemsPerPage))
}

const (
	DefaultPageSize = 20
	MaxPageSize     = 500
)

// NormalizePaging clamps raw paging input into sane bounds.
func NormalizePaging(page, itemsPerPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	size := itemsPerPage
	if size <= 0 {
		size = DefaultPageSize
	} else if size > MaxPageSize {
		size = MaxPageSize
	}
	return page, size
}

func PageOffset(page, itemsPerPage int) int { return (page - 1) * itemsPerPage }

// MapPage converts a page of one item type into another.
func MapPage[A, B any](p Page[A], f func(A) B) Page[B] {
	items := make([]B, len(p.Items))
	for i, a := range p.Items {
		items[i] = f(a)
	}
	return Page[B]{Items: items, CurrentPage: p.CurrentPage, ItemsPerPage: p.ItemsPerPage, TotalItems: p.TotalItems}
}
