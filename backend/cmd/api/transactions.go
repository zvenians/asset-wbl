package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (a app) logout(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	h := sha256.Sum256([]byte(token))
	_, err := a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, hex.EncodeToString(h[:]))
	if err != nil {
		fail(w, 500, "Gagal keluar")
		return
	}
	respond(w, 200, map[string]string{"status": "logged_out"})
}
func (a app) updateAsset(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var q struct{ Name, Condition, UsageStatus string }
	if json.NewDecoder(r.Body).Decode(&q) != nil || strings.TrimSpace(q.Name) == "" || q.Condition == "" || q.UsageStatus == "" {
		fail(w, 422, "Nama, kondisi, dan status wajib diisi")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE assets SET name=$2,condition=$3,usage_status=$4,updated_at=now() WHERE id=$1 AND archived_at IS NULL`, chi.URLParam(r, "id"), q.Name, q.Condition, q.UsageStatus)
	if err != nil {
		fail(w, 422, "Aset gagal diubah")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 404, "Aset tidak ditemukan")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'update','asset',$2,jsonb_build_object('name',$3::text,'condition',$4::text,'usage_status',$5::text))`, who(r).ID, chi.URLParam(r, "id"), q.Name, q.Condition, q.UsageStatus)
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	respond(w, 200, map[string]string{"status": "updated"})
}
func (a app) transferAsset(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var q struct {
		LocationID   string  `json:"location_id"`
		DepartmentID *string `json:"department_id"`
		Reason       string  `json:"reason"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil || q.LocationID == "" || strings.TrimSpace(q.Reason) == "" {
		fail(w, 422, "Lokasi tujuan dan alasan wajib diisi")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	var oldLocation, oldDepartment *string
	err = tx.QueryRow(r.Context(), `SELECT location_id,department_id FROM assets WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, chi.URLParam(r, "id")).Scan(&oldLocation, &oldDepartment)
	if err != nil {
		fail(w, 404, "Aset tidak ditemukan")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE assets SET location_id=$2,department_id=coalesce($3,department_id),updated_at=now() WHERE id=$1`, chi.URLParam(r, "id"), q.LocationID, q.DepartmentID)
	if err != nil {
		fail(w, 422, "Lokasi atau departemen tujuan tidak valid")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO transfers(asset_id,from_location_id,to_location_id,from_department_id,to_department_id,reason,actor_id) VALUES($1,$2,$3,$4,coalesce($5::uuid,$4::uuid),$6,$7)`, chi.URLParam(r, "id"), oldLocation, q.LocationID, oldDepartment, q.DepartmentID, q.Reason, who(r).ID)
	if err != nil {
		fail(w, 500, "Riwayat mutasi gagal")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'transfer','asset',$2,jsonb_build_object('reason',$3::text))`, who(r).ID, chi.URLParam(r, "id"), q.Reason)
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	respond(w, 200, map[string]string{"status": "transferred"})
}
