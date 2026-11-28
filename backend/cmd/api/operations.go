package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func registerOperations(r chi.Router, a app) {
	r.Put("/api/v1/assets/{id}", a.updateAsset)
	r.Post("/api/v1/assets/{id}/transfer", a.transferAsset)
	r.Get("/api/v1/assets/export.xlsx", a.exportAssets)
	r.Post("/api/v1/assets/import.xlsx", a.importAssets)
	r.Get("/api/v1/reporting/assets", a.reporterAssets)
	r.Get("/api/v1/locations/tree", a.locationTree)
	r.Get("/api/v1/maintenance/plans", a.plans)
	r.Get("/api/v1/maintenance/work-orders", a.workOrders)
	r.Post("/api/v1/reports/{id}/review", a.reviewReport)
	r.Post("/api/v1/reports/{id}/photos", a.uploadReportPhoto)
	r.Get("/api/v1/reports/{id}/photos", a.listReportPhotos)
	r.Get("/api/v1/reports/{id}/photos/{photoId}", a.downloadReportPhoto)
	r.Patch("/api/v1/maintenance/work-orders/{id}", a.updateWorkOrder)
	r.Get("/api/v1/notifications", a.notifications)
	r.Patch("/api/v1/notifications/{id}/read", a.readNotification)
	r.Get("/api/v1/documents", a.documents)
	r.Post("/api/v1/documents", a.createDocument)
	r.Post("/api/v1/documents/{id}/versions", a.uploadDocument)
	r.Get("/api/v1/documents/{id}/file", a.downloadDocument)
	r.Get("/api/v1/depreciation/policies", a.depreciationPolicies)
	r.Post("/api/v1/depreciation/policies", a.createDepreciationPolicy)
	r.Post("/api/v1/depreciation/periods", a.generateDepreciation)
	r.Get("/api/v1/depreciation/periods", a.depreciationPeriods)
	r.Post("/api/v1/depreciation/periods/{id}/lock", a.lockDepreciation)
}

func (a app) reporterAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		respond(w, 200, []any{})
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT a.id,a.inventory_no,a.name,coalesce(l.name,'') FROM assets a LEFT JOIN locations l ON l.id=a.location_id WHERE a.archived_at IS NULL AND (a.inventory_no ILIKE '%'||$1||'%' OR a.name ILIKE '%'||$1||'%') ORDER BY a.inventory_no LIMIT 20`, q)
	if err != nil {
		fail(w, 500, "Pencarian gagal")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, no, name, location string
		if rows.Scan(&id, &no, &name, &location) == nil {
			items = append(items, map[string]string{"id": id, "inventory_no": no, "name": name, "location": location})
		}
	}
	respond(w, 200, items)
}

func (a app) plans(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,a.name,p.title,p.next_due,p.active FROM maintenance_plans p JOIN assets a ON a.id=p.asset_id ORDER BY p.next_due LIMIT 100`)
	if err != nil {
		fail(w, 500, "Gagal memuat rencana")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, name, title string
		var due time.Time
		var active bool
		if rows.Scan(&id, &name, &title, &due, &active) == nil {
			items = append(items, map[string]any{"id": id, "asset": name, "title": title, "next_due": due.Format("2006-01-02"), "active": active})
		}
	}
	respond(w, 200, items)
}
func (a app) workOrders(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT w.id,a.name,w.status,w.created_at,w.diagnosis,w.action_taken FROM work_orders w JOIN assets a ON a.id=w.asset_id ORDER BY w.created_at DESC LIMIT 100`)
	if err != nil {
		fail(w, 500, "Gagal memuat pekerjaan")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, name, status string
		var created time.Time
		var diagnosis, action *string
		if rows.Scan(&id, &name, &status, &created, &diagnosis, &action) == nil {
			items = append(items, map[string]any{"id": id, "asset": name, "status": status, "created_at": created, "diagnosis": diagnosis, "action_taken": action})
		}
	}
	respond(w, 200, items)
}
func (a app) reviewReport(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var input struct {
		Accept  bool    `json:"accept"`
		Reason  string  `json:"reason"`
		AssetID *string `json:"asset_id"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		fail(w, 400, "Input tidak valid")
		return
	}
	if !input.Accept && input.Reason == "" {
		fail(w, 422, "Alasan penolakan wajib diisi")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	var assetID *string
	var reporterID string
	var status string
	err = tx.QueryRow(r.Context(), `SELECT asset_id,reporter_id,status FROM damage_reports WHERE id=$1 FOR UPDATE`, chi.URLParam(r, "id")).Scan(&assetID, &reporterID, &status)
	if err != nil {
		fail(w, 404, "Laporan tidak ditemukan")
		return
	}
	if status != "baru" {
		fail(w, 409, "Laporan sudah ditinjau")
		return
	}
	if input.AssetID != nil {
		assetID = input.AssetID
	}
	next := "ditolak"
	if input.Accept {
		next = "diterima"
		if assetID == nil {
			fail(w, 422, "Aset harus diidentifikasi sebelum work order")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE damage_reports SET status=$2,asset_id=$3 WHERE id=$1`, chi.URLParam(r, "id"), next, assetID)
	if err != nil {
		fail(w, 422, "Pembaruan laporan gagal")
		return
	}
	if input.Accept {
		_, err = tx.Exec(r.Context(), `INSERT INTO work_orders(asset_id,report_id,status) VALUES($1,$2,'terjadwal')`, assetID, chi.URLParam(r, "id"))
		if err != nil {
			fail(w, 409, "Work order sudah ada")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO notifications(recipient_id,event_key,title,body,entity_type,entity_id) VALUES($1,$2,$3,$4,'report',$5) ON CONFLICT DO NOTHING`, reporterID, "report-review-"+chi.URLParam(r, "id"), "Laporan ditinjau", next, chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 500, "Notifikasi gagal")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,$2,'report',$3)`, who(r).ID, next, chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	respond(w, 200, map[string]string{"status": next})
}
func (a app) updateWorkOrder(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var input struct {
		Status    string `json:"status"`
		Diagnosis string `json:"diagnosis"`
		Action    string `json:"action_taken"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		fail(w, 400, "Input tidak valid")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	var current string
	var reportID *string
	err = tx.QueryRow(r.Context(), `SELECT status,report_id FROM work_orders WHERE id=$1 FOR UPDATE`, chi.URLParam(r, "id")).Scan(&current, &reportID)
	if err != nil {
		fail(w, 404, "Pekerjaan tidak ditemukan")
		return
	}
	allowed := map[string][]string{"terjadwal": {"dikerjakan"}, "dikerjakan": {"menunggu_material", "verifikasi"}, "menunggu_material": {"dikerjakan"}, "verifikasi": {"selesai", "dikerjakan"}}
	valid := false
	for _, s := range allowed[current] {
		if s == input.Status {
			valid = true
		}
	}
	if !valid {
		fail(w, 409, "Transisi status tidak diizinkan")
		return
	}
	if input.Status == "verifikasi" && (input.Diagnosis == "" || input.Action == "") {
		fail(w, 422, "Diagnosis dan tindakan wajib diisi")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE work_orders SET status=$2,diagnosis=coalesce(nullif($3,''),diagnosis),action_taken=coalesce(nullif($4,''),action_taken) WHERE id=$1`, chi.URLParam(r, "id"), input.Status, input.Diagnosis, input.Action)
	if err != nil {
		fail(w, 500, "Pembaruan gagal")
		return
	}
	if reportID != nil {
		_, err = tx.Exec(r.Context(), `UPDATE damage_reports SET status=$2 WHERE id=$1`, reportID, input.Status)
		if err != nil {
			fail(w, 500, "Pembaruan laporan gagal")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'status','work_order',$2,jsonb_build_object('from',$3::text,'to',$4::text))`, who(r).ID, chi.URLParam(r, "id"), current, input.Status)
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	respond(w, 200, map[string]string{"status": input.Status})
}
func (a app) notifications(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `SELECT id,title,body,read_at,created_at FROM notifications WHERE recipient_id=$1 ORDER BY created_at DESC LIMIT 100`, who(r).ID)
	if err != nil {
		fail(w, 500, "Gagal memuat notifikasi")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, title, body string
		var read *time.Time
		var created time.Time
		if rows.Scan(&id, &title, &body, &read, &created) == nil {
			items = append(items, map[string]any{"id": id, "title": title, "body": body, "read_at": read, "created_at": created})
		}
	}
	respond(w, 200, items)
}
func (a app) readNotification(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(), `UPDATE notifications SET read_at=coalesce(read_at,now()) WHERE id=$1 AND recipient_id=$2`, chi.URLParam(r, "id"), who(r).ID)
	if err != nil || tag.RowsAffected() == 0 {
		fail(w, 404, "Notifikasi tidak ditemukan")
		return
	}
	respond(w, 200, map[string]string{"status": "read"})
}
func (a app) documents(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,coalesce(number,''),title,category,coalesce(expires_on::text,'') FROM documents WHERE sensitivity='internal' OR $1='admin' ORDER BY created_at DESC LIMIT 100`, who(r).Role)
	if err != nil {
		fail(w, 500, "Gagal memuat dokumen")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, no, title, category, expires string
		if rows.Scan(&id, &no, &title, &category, &expires) == nil {
			items = append(items, map[string]string{"id": id, "number": no, "title": title, "category": category, "expires_on": expires})
		}
	}
	respond(w, 200, items)
}
