package main

import "net/http"

func (a app) locationTree(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT l.id,l.name,l.parent_id,b.name,count(a.id) FROM locations l JOIN business_units b ON b.id=l.unit_id LEFT JOIN assets a ON a.location_id=l.id AND a.archived_at IS NULL GROUP BY l.id,b.name ORDER BY b.name,l.parent_id NULLS FIRST,l.name`)
	if err != nil {
		fail(w, 500, "Peta lokasi gagal dimuat")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, name, unit string
		var parent *string
		var count int
		if rows.Scan(&id, &name, &parent, &unit, &count) == nil {
			items = append(items, map[string]any{"id": id, "name": name, "parent_id": parent, "unit": unit, "asset_count": count})
		}
	}
	respond(w, 200, items)
}
