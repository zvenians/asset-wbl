package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (a app) createDocument(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var input struct {
		Number           string  `json:"number"`
		Title            string  `json:"title"`
		Category         string  `json:"category"`
		Sensitivity      string  `json:"sensitivity"`
		ExpiresOn        *string `json:"expires_on"`
		PhysicalLocation string  `json:"physical_location"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&input) != nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Category) == "" {
		fail(w, 422, "Judul dan kategori wajib diisi")
		return
	}
	if input.Sensitivity == "" {
		input.Sensitivity = "internal"
	}
	if input.Sensitivity != "internal" && input.Sensitivity != "sensitive" {
		fail(w, 422, "Klasifikasi tidak valid")
		return
	}
	if input.Sensitivity == "sensitive" && who(r).Role != "admin" {
		fail(w, 403, "Izin dokumen sensitif diperlukan")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(), `INSERT INTO documents(number,title,category,sensitivity,expires_on,physical_location,pic_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, input.Number, input.Title, input.Category, input.Sensitivity, input.ExpiresOn, input.PhysicalLocation, who(r).ID).Scan(&id)
	if err != nil {
		fail(w, 422, "Dokumen gagal disimpan")
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'create','document',$2)`, who(r).ID, id)
	respond(w, 201, map[string]string{"id": id})
}
func (a app) uploadDocument(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var sensitivity string
	if a.db.QueryRow(r.Context(), `SELECT sensitivity FROM documents WHERE id=$1`, chi.URLParam(r, "id")).Scan(&sensitivity) != nil {
		fail(w, 404, "Dokumen tidak ditemukan")
		return
	}
	if sensitivity == "sensitive" && who(r).Role != "admin" {
		fail(w, 403, "Izin dokumen sensitif diperlukan")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 422, "File PDF/JPEG/PNG maksimal 20 MB diperlukan")
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	mime := http.DetectContentType(head[:n])
	if mime != "application/pdf" && mime != "image/jpeg" && mime != "image/png" {
		fail(w, 422, "Tipe file tidak diizinkan")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "Gagal membaca file")
		return
	}
	root := os.Getenv("FILE_STORAGE_DIR")
	if root == "" {
		root = "./private-files"
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		fail(w, 500, "Storage tidak tersedia")
		return
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		fail(w, 500, "Gagal membuat kunci file")
		return
	}
	key := hex.EncodeToString(random)
	target := filepath.Join(root, key)
	dest, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "Gagal menyimpan file")
		return
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(dest, hash), file)
	closeErr := dest.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(target)
		fail(w, 500, "Gagal menyimpan file")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		os.Remove(target)
		fail(w, 500, "Transaksi gagal")
		return
	}
	defer tx.Rollback(r.Context())
	var version int
	err = tx.QueryRow(r.Context(), `SELECT coalesce(max(version_no),0)+1 FROM document_versions WHERE document_id=$1`, chi.URLParam(r, "id")).Scan(&version)
	if err != nil {
		os.Remove(target)
		fail(w, 500, "Versi gagal")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO document_versions(document_id,version_no,storage_key,filename,mime_type,size_bytes,sha256,uploaded_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, chi.URLParam(r, "id"), version, key, filepath.Base(header.Filename), mime, size, hex.EncodeToString(hash.Sum(nil)), who(r).ID).Scan(&id)
	if err != nil {
		os.Remove(target)
		fail(w, 422, "Dokumen tidak ditemukan atau versi gagal")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_log(actor_id,action,entity_type,entity_id) VALUES($1,'upload','document',$2)`, who(r).ID, chi.URLParam(r, "id"))
	if err != nil {
		os.Remove(target)
		fail(w, 500, "Audit gagal")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		os.Remove(target)
		fail(w, 500, "Transaksi gagal")
		return
	}
	respond(w, 201, map[string]any{"id": id, "version": version})
}
func (a app) downloadDocument(w http.ResponseWriter, r *http.Request) {
	if !staff(w, r) {
		return
	}
	var key, name, mime string
	err := a.db.QueryRow(r.Context(), `SELECT v.storage_key,v.filename,v.mime_type FROM document_versions v JOIN documents d ON d.id=v.document_id WHERE v.document_id=$1 AND (d.sensitivity='internal' OR $2='admin') ORDER BY v.version_no DESC LIMIT 1`, chi.URLParam(r, "id"), who(r).Role).Scan(&key, &name, &mime)
	if err != nil {
		fail(w, 404, "File tidak ditemukan")
		return
	}
	root := os.Getenv("FILE_STORAGE_DIR")
	if root == "" {
		root = "./private-files"
	}
	file, err := os.Open(filepath.Join(root, key))
	if err != nil {
		fail(w, 404, "File tidak tersedia")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline; filename="+strings.ReplaceAll(filepath.Base(name), "\"", ""))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, file)
}
