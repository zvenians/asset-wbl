package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
)

var vehicleMoney = regexp.MustCompile(`^(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$`)

func (a app) vehicleRenewals(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var plate string
	var odometer int64
	var annual, five *time.Time
	if a.db.QueryRow(r.Context(), `SELECT plate_no,odometer_km,annual_due,five_year_due FROM vehicles WHERE asset_id=$1`, id).Scan(&plate, &odometer, &annual, &five) != nil {
		fail(w, 404, "Kendaraan tidak ditemukan")
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT kind,old_due,new_due,coalesce(cost::text,''),created_at FROM vehicle_renewals WHERE asset_id=$1 ORDER BY created_at DESC,id DESC`, id)
	if err != nil {
		fail(w, 500, "Gagal memuat riwayat perpanjangan")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var kind, cost string
		var old *time.Time
		var due, created time.Time
		if rows.Scan(&kind, &old, &due, &cost, &created) == nil {
			items = append(items, map[string]any{"kind": kind, "old_due": vehicleDate(old), "new_due": due.Format("2006-01-02"), "cost": cost, "created_at": created})
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "Gagal memuat riwayat perpanjangan")
		return
	}
	respond(w, 200, map[string]any{"plate_no": plate, "odometer_km": odometer, "annual_due": vehicleDate(annual), "five_year_due": vehicleDate(five), "renewals": items})
}

func vehicleDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

func (a app) createVehicleRenewal(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var q struct {
		Kind   string `json:"kind"`
		NewDue string `json:"new_due"`
		Cost   string `json:"cost"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&q) != nil {
		fail(w, 400, "Input tidak valid")
		return
	}
	if q.Kind != "annual" && q.Kind != "five_year" {
		fail(w, 422, "Jenis perpanjangan tidak valid")
		return
	}
	due, err := time.Parse("2006-01-02", q.NewDue)
	if err != nil || due.Format("2006-01-02") != q.NewDue {
		fail(w, 422, "Tanggal jatuh tempo tidak valid")
		return
	}
	if q.Cost != "" && !vehicleMoney.MatchString(q.Cost) {
		fail(w, 422, "Biaya tidak valid")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Gagal menyimpan perpanjangan")
		return
	}
	defer tx.Rollback(r.Context())
	var old *time.Time
	var column string
	if q.Kind == "annual" {
		column = "annual_due"
	} else {
		column = "five_year_due"
	}
	if err = tx.QueryRow(r.Context(), `SELECT `+column+` FROM vehicles WHERE asset_id=$1 FOR UPDATE`, id).Scan(&old); err != nil {
		fail(w, 404, "Kendaraan tidak ditemukan")
		return
	}
	if old != nil && !due.After(*old) {
		fail(w, 422, "Jatuh tempo baru harus setelah tanggal sebelumnya")
		return
	}
	var renewalID string
	if err = tx.QueryRow(r.Context(), `INSERT INTO vehicle_renewals(asset_id,kind,old_due,new_due,cost) VALUES($1,$2,$3,$4,NULLIF($5,'')::numeric) RETURNING id`, id, q.Kind, old, due, q.Cost).Scan(&renewalID); err != nil {
		fail(w, 500, "Gagal mencatat perpanjangan")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE vehicles SET `+column+`=$2 WHERE asset_id=$1`, id, due); err != nil {
		fail(w, 500, "Gagal memperbarui kendaraan")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'renewal','vehicle',$2,jsonb_build_object('kind',$3::text,'new_due',$4::date))`, who(r).ID, id, q.Kind, due); err != nil {
		fail(w, 500, "Gagal mencatat audit")
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail(w, 500, "Gagal menyimpan perpanjangan")
		return
	}
	respond(w, 201, map[string]string{"id": renewalID})
}
