package data

import (
	"fmt"

	"github.com/olmesm/gort/internal/core"
)

type TagRenameError struct {
	NotFound  bool
	NameTaken bool
	Name      string
}

func (e *TagRenameError) Error() string {
	if e.NameTaken {
		return fmt.Sprintf("A tag named '%s' already exists.", e.Name)
	}
	return fmt.Sprintf("Tag '%s' was not found.", e.Name)
}

func TagsForShortUrl(db *Db, shortUrlId core.ShortUrlID) ([]string, error) {
	rows, err := db.Query(
		`SELECT t.name FROM tags t
		 JOIN short_url_tags st ON st.tag_id = t.id
		 WHERE st.short_url_id = ? ORDER BY t.name`, shortUrlId.Value())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// TagsForShortUrls fetches tags for many short URLs at once: id -> tag names.
func TagsForShortUrls(db *Db, shortUrlIds []core.ShortUrlID) (map[int64][]string, error) {
	result := map[int64][]string{}
	if len(shortUrlIds) == 0 {
		return result, nil
	}
	ids := make([]int64, len(shortUrlIds))
	for i, id := range shortUrlIds {
		ids[i] = id.Value()
	}
	inClause, args := InList("st.short_url_id", ids)
	rows, err := db.Query(
		fmt.Sprintf(`SELECT st.short_url_id, t.name FROM tags t
		             JOIN short_url_tags st ON st.tag_id = t.id
		             WHERE %s ORDER BY t.name`, inClause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = append(result[id], name)
	}
	return result, rows.Err()
}

func ListTags(db *Db, searchTerm string, page, itemsPerPage int) (core.Page[TagStatsRow], error) {
	empty := core.Page[TagStatsRow]{}
	whereClause := ""
	var whereArgs []any
	if searchTerm != "" {
		whereClause = fmt.Sprintf("WHERE %s", db.ILike("t.name", "?"))
		whereArgs = append(whereArgs, "%"+searchTerm+"%")
	}

	var total int64
	if err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM tags t %s", whereClause), whereArgs...).Scan(&total); err != nil {
		return empty, err
	}

	listArgs := append(append([]any{}, whereArgs...), itemsPerPage, core.PageOffset(page, itemsPerPage))
	rows, err := db.Query(
		fmt.Sprintf(`SELECT t.id, t.name,
		        (SELECT COUNT(*) FROM short_url_tags st WHERE st.tag_id = t.id) AS short_url_count,
		        (SELECT COUNT(*) FROM visits v
		           JOIN short_url_tags st ON st.short_url_id = v.short_url_id
		          WHERE st.tag_id = t.id) AS visit_count
		 FROM tags t %s
		 ORDER BY t.name
		 LIMIT ? OFFSET ?`, whereClause), listArgs...)
	if err != nil {
		return empty, err
	}
	defer rows.Close()

	items := []TagStatsRow{}
	for rows.Next() {
		var t TagStatsRow
		if err := rows.Scan(&t.Id, &t.Name, &t.ShortUrlCount, &t.VisitCount); err != nil {
			return empty, err
		}
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}

	return core.Page[TagStatsRow]{
		Items:        items,
		CurrentPage:  page,
		ItemsPerPage: itemsPerPage,
		TotalItems:   total,
	}, nil
}

// RenameTag renames a tag; oldName is a caller-supplied candidate, newName is
// validated.
func RenameTag(db *Db, oldName string, newName core.TagName) error {
	var existingNew int64
	if err := db.QueryRow("SELECT COUNT(*) FROM tags WHERE name = ?", newName.Value()).Scan(&existingNew); err != nil {
		return err
	}
	if existingNew > 0 && oldName != newName.Value() {
		return &TagRenameError{NameTaken: true, Name: newName.Value()}
	}
	res, err := db.Exec("UPDATE tags SET name = ? WHERE name = ?", newName.Value(), oldName)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return &TagRenameError{NotFound: true, Name: oldName}
	}
	return nil
}

// DeleteTags deletes tags by candidate names; returns how many existed.
func DeleteTags(db *Db, names []string) (int, error) {
	if len(names) == 0 {
		return 0, nil
	}
	inClause, args := InList("name", names)
	res, err := db.Exec(fmt.Sprintf("DELETE FROM tags WHERE %s", inClause), args...)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

func TagExists(db *Db, name string) (bool, error) {
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM tags WHERE name = ?", name).Scan(&count)
	return count > 0, err
}

func ListAllTagNames(db *Db) ([]string, error) {
	rows, err := db.Query("SELECT name FROM tags ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
