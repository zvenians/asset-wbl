package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/zvenians/asset-wbl/backend/internal/depreciation"
)

func adminOnly(w http.ResponseWriter, r *http.Request) bool {
	if who(r).Role != "admin" {
		fail(w, 403, "Akses keuangan hanya untuk administrator")
		return false
	}
	return true
}
func cents(s string) (int64, error) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, errors.New("invalid money")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	frac := "00"
	if len(parts) == 2 {
		frac = parts[1]
		if len(frac) == 1 {
			frac += "0"
		}
		if len(frac) != 2 {
			return 0, errors.New("invalid cents")
		}
	}
	decimal, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}
	if whole > 92233720368547758 {
		return 0, errors.New("money overflow")
	}
	return whole*100 + decimal, nil
}
func money(c int64) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }

func (a app) depreciationPolicies(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,c.name,p.version,p.method,p.life_months,p.residual::text,p.effective_on,p.simulation FROM depreciation_policies p JOIN categories c ON c.id=p.category_id ORDER BY c.name,p.version DESC`)
	if err != nil {
		fail(w, 500, "Kebijakan gagal dimuat")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, category, method, residual string
		var version int
		var life *int
		var effective time.Time
		var simulation bool
		if rows.Scan(&id, &category, &version, &method, &life, &residual, &effective, &simulation) == nil {
			items = append(items, map[string]any{"id": id, "category": category, "version": version, "method": method, "life_months": life, "residual": residual, "effective_on": effective.Format("2006-01-02"), "simulation": simulation})
		}
	}
	respond(w, 200, items)
}

func (a app) createDepreciationPolicy(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	var v struct {
		CategoryID  string `json:"category_id"`
		Method      string `json:"method"`
		LifeMonths  *int   `json:"life_months"`
		Residual    string `json:"residual"`
		EffectiveOn string `json:"effective_on"`
	}
	if json.NewDecoder(r.Body).Decode(&v) != nil || v.CategoryID == "" {
		fail(w, 422, "Kategori dan kebijakan wajib diisi")
		return
	}
	if v.Method != "straight_line" && v.Method != "declining" && v.Method != "double_declining" && v.Method != "none" {
		fail(w, 422, "Metode tidak valid")
		return
	}
	if v.Method != "none" && (v.LifeMonths == nil || *v.LifeMonths <= 0) {
		fail(w, 422, "Umur manfaat wajib diisi")
		return
	}
	if v.Residual == "" {
		v.Residual = "0.00"
	}
	if amount, err := cents(v.Residual); err != nil || amount < 0 {
		fail(w, 422, "Nilai residu tidak valid")
		return
	}
	effective, err := time.Parse("2006-01-02", v.EffectiveOn)
	if err != nil || effective.Day() != 1 {
		fail(w, 422, "Tanggal berlaku harus hari pertama bulan")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, v.CategoryID); err != nil {
		fail(w, 500, "Kebijakan gagal dikunci")
		return
	}
	var lastVersion int
	var lastDate *time.Time
	err = tx.QueryRow(r.Context(), `SELECT coalesce(max(version),0),max(effective_on) FROM depreciation_policies WHERE category_id=$1 AND book='commercial'`, v.CategoryID).Scan(&lastVersion, &lastDate)
	if err != nil {
		fail(w, 422, "Kategori tidak valid")
		return
	}
	if lastDate != nil && !effective.After(*lastDate) {
		fail(w, 409, "Tanggal berlaku harus setelah versi sebelumnya")
		return
	}
	var locked *time.Time
	err = tx.QueryRow(r.Context(), `SELECT max(period_start) FROM depreciation_periods WHERE book='commercial' AND locked_at IS NOT NULL`).Scan(&locked)
	if err != nil {
		fail(w, 500, "Periode gagal diperiksa")
		return
	}
	if locked != nil && !effective.After(*locked) {
		fail(w, 409, "Periode telah dikunci")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO depreciation_policies(category_id,book,version,method,life_months,residual,effective_on,simulation) VALUES($1,'commercial',$2,$3,$4,$5,$6,true) RETURNING id`, v.CategoryID, lastVersion+1, v.Method, v.LifeMonths, v.Residual, v.EffectiveOn).Scan(&id)
	if err != nil {
		fail(w, 422, "Kebijakan gagal disimpan")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'create','depreciation_policy',$2,jsonb_build_object('simulation',true))`, who(r).ID, id)
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Kebijakan gagal disimpan")
		return
	}
	respond(w, 201, map[string]any{"id": id, "version": lastVersion + 1, "simulation": true})
}

func (a app) generateDepreciation(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	var input struct {
		Period string `json:"period"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		fail(w, 422, "Periode wajib diisi")
		return
	}
	start, err := time.Parse("2006-01", input.Period)
	if err != nil || start.Before(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		fail(w, 422, "Periode simulasi mulai November 2026")
		return
	}
	period := start.Format("2006-01-02")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('wbl-depreciation-commercial'))`); err != nil {
		fail(w, 500, "Penguncian gagal")
		return
	}
	var id string
	var locked *time.Time
	err = tx.QueryRow(r.Context(), `INSERT INTO depreciation_periods(book,period_start) VALUES('commercial',$1) ON CONFLICT(book,period_start) DO UPDATE SET book=EXCLUDED.book RETURNING id,locked_at`, period).Scan(&id, &locked)
	if err != nil {
		fail(w, 500, "Periode gagal dibuat")
		return
	}
	if locked != nil {
		fail(w, 409, "Periode telah dikunci")
		return
	}
	var existing int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM depreciation_entries WHERE period_id=$1`, id).Scan(&existing); err != nil {
		fail(w, 500, "Ledger gagal diperiksa")
		return
	}
	if existing > 0 {
		respond(w, 200, map[string]any{"id": id, "period": input.Period, "entries": existing, "status": "existing"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT a.id,a.purchase_cost::text,a.opening_accumulated::text,a.depreciation_starts_on,p.id,p.method,p.life_months,p.residual::text FROM assets a JOIN LATERAL (SELECT p.id,p.method,p.life_months,p.residual FROM depreciation_policies p WHERE p.category_id=a.category_id AND p.book='commercial' AND p.effective_on<=$1 ORDER BY p.effective_on DESC,p.version DESC LIMIT 1) p ON true WHERE a.archived_at IS NULL AND a.purchase_cost IS NOT NULL AND a.depreciation_starts_on<=$1 ORDER BY a.id`, period)
	if err != nil {
		fail(w, 500, "Aset penyusutan gagal dimuat")
		return
	}
	type record struct {
		id, policy, cost, opening, residual, method string
		starts                                      time.Time
		life                                        *int
	}
	records := []record{}
	for rows.Next() {
		var v record
		if rows.Scan(&v.id, &v.cost, &v.opening, &v.starts, &v.policy, &v.method, &v.life, &v.residual) != nil {
			rows.Close()
			fail(w, 500, "Aset penyusutan gagal dibaca")
			return
		}
		records = append(records, v)
	}
	rows.Close()
	if rows.Err() != nil {
		fail(w, 500, "Aset penyusutan gagal dibaca")
		return
	}
	total := int64(0)
	for _, v := range records {
		cost, err := cents(v.cost)
		if err != nil {
			fail(w, 422, "Nilai aset tidak valid")
			return
		}
		opening, err := cents(v.opening)
		if err != nil {
			fail(w, 422, "Akumulasi awal tidak valid")
			return
		}
		residual, err := cents(v.residual)
		if err != nil {
			fail(w, 422, "Nilai residu tidak valid")
			return
		}
		elapsed := (start.Year()-v.starts.Year())*12 + int(start.Month()-v.starts.Month())
		var previous string
		err = tx.QueryRow(r.Context(), `SELECT e.accumulated::text FROM depreciation_entries e JOIN depreciation_periods p ON p.id=e.period_id WHERE e.asset_id=$1 AND p.period_start<$2 ORDER BY p.period_start DESC LIMIT 1`, v.id, period).Scan(&previous)
		accumulated := opening
		if err == nil {
			accumulated, err = cents(previous)
			if err != nil {
				fail(w, 500, "Ledger tidak valid")
				return
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			fail(w, 500, "Ledger gagal dibaca")
			return
		} else if elapsed > 0 && v.method != "none" {
			fail(w, 409, "Hitung periode sebelumnya terlebih dahulu")
			return
		}
		life := 0
		if v.life != nil {
			life = *v.life
		}
		result, err := depreciation.Calculate(depreciation.Input{Cost: cost, Residual: residual, Accumulated: accumulated, LifeMonths: life, ElapsedMonths: elapsed, Method: depreciation.Method(v.method)})
		if err != nil {
			fail(w, 422, "Kebijakan penyusutan tidak valid")
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO depreciation_entries(asset_id,period_id,policy_id,amount,accumulated,book_value) VALUES($1,$2,$3,$4,$5,$6)`, v.id, id, v.policy, money(result.Charge), money(result.Accumulated), money(result.BookValue))
		if err != nil {
			fail(w, 500, "Ledger gagal disimpan")
			return
		}
		total += result.Charge
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'generate','depreciation_period',$2,jsonb_build_object('entries',$3::integer))`, who(r).ID, id, len(records))
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Ledger gagal disimpan")
		return
	}
	respond(w, 201, map[string]any{"id": id, "period": input.Period, "entries": len(records), "charge": money(total), "simulation": true})
}

func (a app) depreciationPeriods(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT p.id,p.period_start,p.locked_at,count(e.id),coalesce(sum(e.amount),0)::text FROM depreciation_periods p LEFT JOIN depreciation_entries e ON e.period_id=p.id WHERE p.book='commercial' GROUP BY p.id ORDER BY p.period_start DESC LIMIT 36`)
	if err != nil {
		fail(w, 500, "Periode gagal dimuat")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, total string
		var period time.Time
		var locked *time.Time
		var count int
		if rows.Scan(&id, &period, &locked, &count, &total) == nil {
			items = append(items, map[string]any{"id": id, "period": period.Format("2006-01"), "locked": locked != nil, "entries": count, "charge": total, "simulation": true})
		}
	}
	respond(w, 200, items)
}
func (a app) lockDepreciation(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `UPDATE depreciation_periods SET locked_at=now() WHERE id=$1 AND locked_at IS NULL AND EXISTS(SELECT 1 FROM depreciation_entries WHERE period_id=$1)`, chi.URLParam(r, "id"))
	if err != nil || tag.RowsAffected() == 0 {
		fail(w, 409, "Periode kosong atau sudah dikunci")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'lock','depreciation_period',$2)`, who(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		fail(w, 500, "Penguncian gagal")
		return
	}
	respond(w, 200, map[string]string{"status": "locked"})
}
