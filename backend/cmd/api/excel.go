package main

import (
	"net/http"
	"strings"

	"github.com/xuri/excelize/v2"
)

var assetColumns = []string{"Nomor Inventaris", "Nama Aset", "Kategori", "Unit Bisnis", "Kondisi", "Status"}

type importRow struct {
	Number    string `json:"inventory_no"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Unit      string `json:"unit"`
	Condition string `json:"condition"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (a app) exportAssets(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT a.inventory_no,a.name,c.name,b.name,a.condition,a.usage_status FROM assets a JOIN categories c ON c.id=a.category_id JOIN business_units b ON b.id=a.unit_id WHERE a.archived_at IS NULL ORDER BY a.inventory_no LIMIT 10000`)
	if err != nil {
		fail(w, 500, "Gagal memuat aset")
		return
	}
	defer rows.Close()
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	for i, v := range assetColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellStr(sheet, cell, v)
	}
	line := 2
	for rows.Next() {
		var v [6]string
		if rows.Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5]) != nil {
			fail(w, 500, "Gagal membaca aset")
			return
		}
		for i, x := range v {
			cell, _ := excelize.CoordinatesToCellName(i+1, line)
			_ = f.SetCellStr(sheet, cell, x)
		}
		line++
	}
	if rows.Err() != nil {
		fail(w, 500, "Gagal membaca aset")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="wbl-aset-simulasi.xlsx"`)
	if err = f.Write(w); err != nil {
		return
	}
}

func (a app) importAssets(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(w, 422, "Pilih file XLSX maksimal 6 MB")
		return
	}
	defer file.Close()
	f, err := excelize.OpenReader(file)
	if err != nil {
		fail(w, 422, "File XLSX tidak valid")
		return
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		fail(w, 422, "Sheet tidak ditemukan")
		return
	}
	lines, err := f.GetRows(sheets[0])
	if err != nil {
		fail(w, 422, "Sheet gagal dibaca")
		return
	}
	if len(lines) < 2 || len(lines) > 2001 {
		fail(w, 422, "File harus berisi 1–2000 aset")
		return
	}
	if len(lines[0]) < 6 {
		fail(w, 422, "Enam kolom wajib tersedia")
		return
	}
	for i, v := range assetColumns {
		if strings.TrimSpace(lines[0][i]) != v {
			fail(w, 422, "Header Excel tidak sesuai template ekspor")
			return
		}
	}
	cats := map[string]string{}
	units := map[string]string{}
	existing := map[string]bool{}
	cr, err := a.db.Query(r.Context(), `SELECT name,id FROM categories`)
	if err != nil {
		fail(w, 500, "Kategori gagal dimuat")
		return
	}
	for cr.Next() {
		var name, id string
		if cr.Scan(&name, &id) == nil {
			cats[strings.ToLower(name)] = id
		}
	}
	cr.Close()
	ur, err := a.db.Query(r.Context(), `SELECT name,id FROM business_units`)
	if err != nil {
		fail(w, 500, "Unit gagal dimuat")
		return
	}
	for ur.Next() {
		var name, id string
		if ur.Scan(&name, &id) == nil {
			units[strings.ToLower(name)] = id
		}
	}
	ur.Close()
	er, err := a.db.Query(r.Context(), `SELECT inventory_no FROM assets`)
	if err != nil {
		fail(w, 500, "Nomor aset gagal dimuat")
		return
	}
	for er.Next() {
		var no string
		if er.Scan(&no) == nil {
			existing[no] = true
		}
	}
	er.Close()
	reviewed := make([]importRow, 0, len(lines)-1)
	seen := map[string]bool{}
	invalid := 0
	for _, line := range lines[1:] {
		v := make([]string, 6)
		for j := 0; j < 6 && j < len(line); j++ {
			v[j] = strings.TrimSpace(line[j])
		}
		row := importRow{Number: v[0], Name: v[1], Category: v[2], Unit: v[3], Condition: v[4], Status: v[5]}
		issues := []string{}
		if row.Number == "" || row.Name == "" {
			issues = append(issues, "Nomor dan nama wajib")
		}
		if existing[row.Number] || seen[row.Number] {
			issues = append(issues, "Nomor inventaris ganda")
		}
		if cats[strings.ToLower(row.Category)] == "" {
			issues = append(issues, "Kategori tidak dikenal")
		}
		if units[strings.ToLower(row.Unit)] == "" {
			issues = append(issues, "Unit tidak dikenal")
		}
		if row.Condition == "" || row.Status == "" {
			issues = append(issues, "Kondisi dan status wajib")
		}
		row.Error = strings.Join(issues, "; ")
		if row.Error != "" {
			invalid++
		}
		seen[row.Number] = true
		reviewed = append(reviewed, row)
	}
	if r.URL.Query().Get("commit") != "1" {
		respond(w, 200, map[string]any{"rows": reviewed, "invalid": invalid, "valid": len(reviewed) - invalid})
		return
	}
	if invalid > 0 {
		respond(w, 422, map[string]any{"error": "Perbaiki baris bermasalah sebelum import", "rows": reviewed, "invalid": invalid})
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	for _, v := range reviewed {
		var id string
		err = tx.QueryRow(r.Context(), `INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, v.Number, v.Name, cats[strings.ToLower(v.Category)], units[strings.ToLower(v.Unit)], v.Condition, v.Status).Scan(&id)
		if err != nil {
			fail(w, 409, "Nomor inventaris ganda atau data tidak valid; import dibatalkan")
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'import','asset',$2)`, who(r).ID, id)
		if err != nil {
			fail(w, 500, "Audit gagal; import dibatalkan")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Import gagal")
		return
	}
	respond(w, 201, map[string]any{"imported": len(reviewed)})
}
