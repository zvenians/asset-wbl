package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type app struct{ db *pgxpool.Pool }
type identity struct{ ID, Role string }
type keyType int

var loginAttempts = struct {
	sync.Mutex
	entries map[string]struct {
		count int
		until time.Time
	}
}{entries: make(map[string]struct {
	count int
	until time.Time
})}

const identityKey keyType = 1

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}
func (a app) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if t == "" {
			fail(w, 401, "Login diperlukan")
			return
		}
		h := sha256.Sum256([]byte(t))
		var id identity
		err := a.db.QueryRow(r.Context(), "SELECT u.id,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()", hex.EncodeToString(h[:])).Scan(&id.ID, &id.Role)
		if err != nil {
			fail(w, 401, "Sesi tidak valid")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, id)))
	})
}
func who(r *http.Request) identity { return r.Context().Value(identityKey).(identity) }
func staff(w http.ResponseWriter, r *http.Request) bool {
	if who(r).Role == "reporter" {
		fail(w, 403, "Akses ditolak")
		return false
	}
	return true
}
func (a app) login(w http.ResponseWriter, r *http.Request) {
	host, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	if splitErr != nil {
		host = r.RemoteAddr
	}
	loginAttempts.Lock()
	record := loginAttempts.entries[host]
	if time.Now().After(record.until) {
		record.count = 0
		record.until = time.Now().Add(time.Minute)
	}
	if record.count >= 10 {
		loginAttempts.Unlock()
		fail(w, 429, "Terlalu banyak percobaan masuk; coba lagi dalam satu menit")
		return
	}
	record.count++
	loginAttempts.entries[host] = record
	loginAttempts.Unlock()
	var q struct{ Email, Password string }
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		fail(w, 400, "Input tidak valid")
		return
	}
	var id, role, hash string
	err := a.db.QueryRow(r.Context(), "SELECT id,role,password_hash FROM users WHERE email=$1", strings.ToLower(strings.TrimSpace(q.Email))).Scan(&id, &role, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(q.Password)) != nil {
		fail(w, 401, "Email atau kata sandi salah")
		return
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		fail(w, 500, "Gagal membuat sesi")
		return
	}
	token := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	_, err = a.db.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '12 hours')", hex.EncodeToString(h[:]), id)
	if err != nil {
		fail(w, 500, "Gagal menyimpan sesi")
		return
	}
	respond(w, 200, map[string]string{"token": token, "role": role})
}
func (a app) me(w http.ResponseWriter, r *http.Request) { id := who(r); respond(w, 200, id) }
func (a app) dashboard(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var total, vehicles, openWO int
	var cost string
	err := a.db.QueryRow(r.Context(), "SELECT count(*),count(*) FILTER(WHERE c.kind='vehicle'),coalesce(sum(a.purchase_cost),0)::text FROM assets a JOIN categories c ON c.id=a.category_id WHERE a.archived_at IS NULL").Scan(&total, &vehicles, &cost)
	if err != nil {
		fail(w, 500, "Gagal memuat ringkasan")
		return
	}
	_ = a.db.QueryRow(r.Context(), "SELECT count(*) FROM work_orders WHERE status NOT IN ('selesai','ditolak')").Scan(&openWO)
	respond(w, 200, map[string]any{"total_assets": total, "vehicles": vehicles, "open_work_orders": openWO, "purchase_cost": cost, "simulation": true})
}

type assetInput struct {
	InventoryNo  string  `json:"inventory_no"`
	Name         string  `json:"name"`
	CategoryID   string  `json:"category_id"`
	UnitID       string  `json:"unit_id"`
	LocationID   *string `json:"location_id"`
	Condition    string  `json:"condition"`
	UsageStatus  string  `json:"usage_status"`
	PurchaseCost *string `json:"purchase_cost"`
}

func (a app) assets(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	switch r.Method {
	case "GET":
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		if page > 100000 {
			page = 100000
		}
		q := r.URL.Query().Get("q")
		kind := r.URL.Query().Get("kind")
		if kind != "vehicle" && kind != "property" && kind != "" {
			fail(w, 422, "Jenis aset tidak valid")
			return
		}
		filter := " FROM assets a JOIN categories c ON c.id=a.category_id JOIN business_units b ON b.id=a.unit_id WHERE a.archived_at IS NULL AND ($1='' OR a.inventory_no ILIKE '%'||$1||'%' OR a.name ILIKE '%'||$1||'%') AND ($2='' OR ($2='property' AND c.kind IN ('land','building')) OR c.kind=$2)"
		var total int
		if err := a.db.QueryRow(r.Context(), "SELECT count(*)"+filter, q, kind).Scan(&total); err != nil {
			fail(w, 500, "Gagal menghitung aset")
			return
		}
		rows, err := a.db.Query(r.Context(), "SELECT a.id,a.inventory_no,a.name,c.name,c.kind,b.name,a.condition,a.usage_status"+filter+" ORDER BY a.created_at DESC LIMIT 25 OFFSET $3", q, kind, (page-1)*25)
		if err != nil {
			fail(w, 500, "Gagal memuat aset")
			return
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var id, no, name, category, kind, unit, condition, status string
			if rows.Scan(&id, &no, &name, &category, &kind, &unit, &condition, &status) == nil {
				items = append(items, map[string]string{"id": id, "inventory_no": no, "name": name, "category": category, "kind": kind, "unit": unit, "condition": condition, "usage_status": status})
			}
		}
		respond(w, 200, map[string]any{"items": items, "page": page, "total": total, "page_size": 25})
	case "POST":
		var v assetInput
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.InventoryNo == "" || v.Name == "" || v.CategoryID == "" || v.UnitID == "" {
			fail(w, 422, "Nomor, nama, kategori, dan unit wajib diisi")
			return
		}
		if v.Condition == "" {
			v.Condition = "baik"
		}
		if v.UsageStatus == "" {
			v.UsageStatus = "aktif"
		}
		var id string
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			fail(w, 500, "Transaksi gagal")
			return
		}
		defer tx.Rollback(r.Context())
		err = tx.QueryRow(r.Context(), "INSERT INTO assets(inventory_no,name,category_id,unit_id,location_id,condition,usage_status,purchase_cost) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id", v.InventoryNo, v.Name, v.CategoryID, v.UnitID, v.LocationID, v.Condition, v.UsageStatus, v.PurchaseCost).Scan(&id)
		if err != nil {
			fail(w, 409, "Aset gagal disimpan; periksa nomor unik dan referensi")
			return
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'create','asset',$2)", who(r).ID, id); err != nil {
			fail(w, 500, "Audit gagal")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			fail(w, 500, "Transaksi gagal")
			return
		}
		respond(w, 201, map[string]string{"id": id})
	default:
		w.WriteHeader(405)
	}
}
func (a app) asset(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	switch r.Method {
	case "GET":
		var no, name, category, kind, unit, condition, status string
		err := a.db.QueryRow(r.Context(), "SELECT a.inventory_no,a.name,c.name,c.kind,b.name,a.condition,a.usage_status FROM assets a JOIN categories c ON c.id=a.category_id JOIN business_units b ON b.id=a.unit_id WHERE a.id=$1 AND a.archived_at IS NULL", id).Scan(&no, &name, &category, &kind, &unit, &condition, &status)
		if err != nil {
			fail(w, 404, "Aset tidak ditemukan")
			return
		}
		respond(w, 200, map[string]string{"id": id, "inventory_no": no, "name": name, "category": category, "kind": kind, "unit": unit, "condition": condition, "usage_status": status})
	case "DELETE":
		tx, err := a.db.Begin(r.Context())
		if err != nil {
			fail(w, 500, "Transaksi gagal")
			return
		}
		defer tx.Rollback(r.Context())
		tag, err := tx.Exec(r.Context(), "UPDATE assets SET archived_at=now(),updated_at=now() WHERE id=$1 AND archived_at IS NULL", id)
		if err != nil || tag.RowsAffected() == 0 {
			fail(w, 404, "Aset tidak ditemukan")
			return
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'archive','asset',$2)", who(r).ID, id); err != nil {
			fail(w, 500, "Audit gagal")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			fail(w, 500, "Transaksi gagal")
			return
		}
		respond(w, 200, map[string]string{"status": "archived"})
	default:
		w.WriteHeader(405)
	}
}
func (a app) master(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	kind := chi.URLParam(r, "kind")
	var sql string
	switch kind {
	case "categories":
		sql = "SELECT id,name,kind FROM categories ORDER BY name"
	case "units":
		sql = "SELECT id,name,'' FROM business_units ORDER BY name"
	case "locations":
		sql = "SELECT id,name,'' FROM locations ORDER BY name"
	default:
		fail(w, 404, "Master tidak ditemukan")
		return
	}
	rows, err := a.db.Query(r.Context(), sql)
	if err != nil {
		fail(w, 500, "Gagal memuat master")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, name, extra string
		if rows.Scan(&id, &name, &extra) == nil {
			items = append(items, map[string]string{"id": id, "name": name, "kind": extra})
		}
	}
	respond(w, 200, items)
}
func (a app) reports(w http.ResponseWriter, r *http.Request) {
	id := who(r)
	rows, err := a.db.Query(r.Context(), "SELECT id,asset_id,description,location_text,urgency,status,created_at FROM damage_reports WHERE $1<>'reporter' OR reporter_id=$2 ORDER BY created_at DESC LIMIT 100", id.Role, id.ID)
	if err != nil {
		fail(w, 500, "Gagal memuat laporan")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var reportID, description, location, urgency, status string
		var assetID *string
		var created time.Time
		if rows.Scan(&reportID, &assetID, &description, &location, &urgency, &status, &created) == nil {
			items = append(items, map[string]any{"id": reportID, "asset_id": assetID, "description": description, "location": location, "urgency": urgency, "status": status, "created_at": created})
		}
	}
	respond(w, 200, items)
}
func (a app) createReport(w http.ResponseWriter, r *http.Request) {
	var q struct {
		AssetID     *string `json:"asset_id"`
		Location    string  `json:"location"`
		Description string  `json:"description"`
		Urgency     string  `json:"urgency"`
	}
	if json.NewDecoder(r.Body).Decode(&q) != nil || len(q.Description) < 10 || q.Location == "" {
		fail(w, 422, "Lokasi dan uraian minimal 10 karakter wajib diisi")
		return
	}
	if q.Urgency != "rendah" && q.Urgency != "sedang" && q.Urgency != "tinggi" {
		fail(w, 422, "Urgensi tidak valid")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(), "INSERT INTO damage_reports(reporter_id,asset_id,location_text,description,urgency) VALUES($1,$2,$3,$4,$5) RETURNING id", who(r).ID, q.AssetID, q.Location, q.Description, q.Urgency).Scan(&id)
	if err != nil {
		fail(w, 422, "Laporan gagal disimpan")
		return
	}
	respond(w, 201, map[string]string{"id": id, "status": "baru"})
}
func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL required")
	}
	db, err := pgxpool.New(context.Background(), url)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(context.Background()); err != nil {
		log.Fatal(err)
	}
	a := app{db}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			origin := os.Getenv("WEB_ORIGIN")
			if req.Header.Get("Origin") == origin && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			}
			if req.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	r.Post("/api/v1/auth/login", a.login)
	r.Group(func(r chi.Router) {
		r.Use(a.auth)
		r.Get("/api/v1/auth/me", a.me)
		r.Post("/api/v1/auth/logout", a.logout)
		r.Get("/api/v1/dashboard", a.dashboard)
		r.MethodFunc("GET", "/api/v1/assets", a.assets)
		r.MethodFunc("POST", "/api/v1/assets", a.assets)
		r.MethodFunc("GET", "/api/v1/assets/{id}", a.asset)
		r.MethodFunc("DELETE", "/api/v1/assets/{id}", a.asset)
		r.Get("/api/v1/master/{kind}", a.master)
		r.Get("/api/v1/reports", a.reports)
		r.Post("/api/v1/reports", a.createReport)
		registerOperations(r, a)
	})
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("API listening %s", addr)
	err = http.ListenAndServe(addr, r)
	if !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	_ = pgx.ErrNoRows
}
