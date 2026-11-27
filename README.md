# WBL Asset Management Phase 01

Implementasi awal monorepo. Belum merupakan demo Phase 01 lengkap atau siap produksi. Data operasional harus berupa simulasi.

Tersedia skema PostgreSQL awal, API Go untuk login/aset/laporan, workspace React untuk registrasi aset awal, dan portal Vue untuk pelaporan kerusakan. Modul lain dalam PRD belum selesai.

## Menjalankan lokal

Prasyarat: Go 1.24+, PostgreSQL 17, pnpm, dan Goose.

1. Salin `.env.example` ke `.env` dan sesuaikan. Jangan commit `.env`.
2. Jalankan `docker compose up -d`.
3. Jalankan migrasi dengan `goose -dir backend/migrations postgres "$DATABASE_URL" up`.
4. Jalankan `cd backend && go mod tidy && go run ./cmd/api`.
5. Jalankan `pnpm install`, `pnpm dev:workspace`, dan `pnpm dev:portal` pada terminal terpisah.

Migrasi kedua membuat akun demo lokal: `admin@demo.wbl.test` / `DemoAdmin!2026`, `staff@demo.wbl.test` / `DemoStaff!2026`, dan `reporter@demo.wbl.test` / `DemoReport!2026`. Kredensial ini hanya untuk lingkungan development. Deployment lama di `asset-wbl.vercel.app` belum diperbarui dengan implementasi ini. API Go, worker, database, dan storage privat memerlukan hosting terpisah dari frontend statis Vercel.
