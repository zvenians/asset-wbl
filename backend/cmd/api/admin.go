package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

var adminEmail = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (a app) listUsers(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,email,full_name,role,created_at FROM users ORDER BY created_at DESC,id DESC LIMIT 200`)
	if err != nil {
		fail(w, 500, "Gagal memuat pengguna")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, email, name, role string
		var created time.Time
		if rows.Scan(&id, &email, &name, &role, &created) == nil {
			items = append(items, map[string]any{"id": id, "email": email, "name": name, "role": role, "created_at": created})
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "Gagal memuat pengguna")
		return
	}
	respond(w, 200, items)
}

func (a app) createUser(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	var q struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&q) != nil {
		fail(w, 400, "Input tidak valid")
		return
	}
	q.Email = strings.ToLower(strings.TrimSpace(q.Email))
	q.Name = strings.TrimSpace(q.Name)
	if !adminEmail.MatchString(q.Email) || len(q.Name) < 2 || len(q.Name) > 120 || len(q.Password) < 12 || len(q.Password) > 72 || !validRole(q.Role) {
		fail(w, 422, "Email, nama, peran, atau kata sandi tidak valid (minimal 12 karakter)")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(q.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 500, "Gagal mengamankan kata sandi")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Gagal membuat pengguna")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	if err = tx.QueryRow(r.Context(), `INSERT INTO users(email,full_name,password_hash,role) VALUES($1,$2,$3,$4) ON CONFLICT(email) DO NOTHING RETURNING id`, q.Email, q.Name, string(hash), q.Role).Scan(&id); err != nil {
		fail(w, 409, "Email sudah digunakan atau data tidak valid")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'create','user',$2,jsonb_build_object('role',$3::text))`, who(r).ID, id, q.Role); err != nil {
		fail(w, 500, "Gagal mencatat perubahan")
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail(w, 500, "Gagal menyimpan pengguna")
		return
	}
	respond(w, 201, map[string]string{"id": id})
}

func validRole(role string) bool { return role == "admin" || role == "staff" || role == "reporter" }

func (a app) changeUserRole(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var q struct {
		Role string `json:"role"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&q) != nil || !validRole(q.Role) {
		fail(w, 422, "Peran tidak valid")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Gagal mengubah peran")
		return
	}
	defer tx.Rollback(r.Context())
	var old string
	if err = tx.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&old); err != nil {
		fail(w, 404, "Pengguna tidak ditemukan")
		return
	}
	if id == who(r).ID && q.Role != "admin" {
		fail(w, 422, "Tidak dapat menurunkan peran sendiri")
		return
	}
	if old == q.Role {
		respond(w, 200, map[string]string{"role": old})
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET role=$2 WHERE id=$1`, id, q.Role); err != nil {
		fail(w, 500, "Gagal mengubah peran")
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		fail(w, 500, "Gagal mengakhiri sesi lama")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id,changes) VALUES($1,'change_role','user',$2,jsonb_build_object('from',$3::text,'to',$4::text))`, who(r).ID, id, old, q.Role); err != nil {
		fail(w, 500, "Gagal mencatat perubahan")
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail(w, 500, "Gagal menyimpan peran")
		return
	}
	respond(w, 200, map[string]string{"role": q.Role})
}

func (a app) listAudit(w http.ResponseWriter, r *http.Request) {
	if !adminOnly(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT l.id,coalesce(u.full_name,'Sistem'),l.action,l.entity_type,l.entity_id,l.changes,l.created_at FROM audit_log l LEFT JOIN users u ON u.id=l.actor_id ORDER BY l.created_at DESC,l.id DESC LIMIT 200`)
	if err != nil {
		fail(w, 500, "Gagal memuat audit trail")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, actor, action, entity string
		var entityID *string
		var changes json.RawMessage
		var created time.Time
		if rows.Scan(&id, &actor, &action, &entity, &entityID, &changes, &created) == nil {
			items = append(items, map[string]any{"id": id, "actor": actor, "action": action, "entity": entity, "entity_id": entityID, "changes": changes, "created_at": created})
		}
	}
	if rows.Err() != nil {
		fail(w, 500, "Gagal memuat audit trail")
		return
	}
	respond(w, 200, items)
}
