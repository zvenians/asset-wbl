# WBL Asset Management Phase 01

Monorepo demo operasional WBL. **Implementasi Phase 01 belum lengkap dan belum layak produksi.** Seluruh identitas dan data seed adalah simulasi.

Tersedia API Go/Chi untuk login, aset, mutasi, import/export Excel dengan review duplikat, laporan kerusakan dan foto privat, work order, dokumen privat, dan notifikasi; workspace React dengan label QR aset; portal Vue dengan pemindaian QR kamera pada browser yang mendukung BarcodeDetector; serta worker preventive maintenance dan email SMTP. Migrasi demo mengisi 365 aset, 50 rencana preventive, dan 30 laporan simulasi. Fungsi penyusutan Go sudah diuji unit, tetapi belum terhubung ke ledger dan antarmuka.

## Menjalankan lokal

Prasyarat: Go 1.26+, PostgreSQL 17, pnpm, dan Goose.

1. Salin `.env.example` ke `.env` dan sesuaikan. Jangan commit `.env`.
2. Jalankan `docker compose up -d`.
3. Jalankan migrasi dengan `goose -dir backend/migrations postgres "$DATABASE_URL" up`.
4. Jalankan `cd backend && go run ./cmd/api` dan `cd backend && go run ./cmd/worker` pada terminal terpisah. Pastikan variabel lingkungan dari `.env` sudah diekspor ke proses; PowerShell tidak memuatnya otomatis.
5. Jalankan `pnpm install`, `pnpm dev:workspace`, dan `pnpm dev:portal` pada terminal terpisah.

Mailpit dapat dibuka di `http://localhost:8025`. Pada demo, email hanya dikirim ke `DEMO_EMAIL_ALLOWLIST`. Uji build dengan `pnpm build` dan `cd backend && go test ./...`.

Migrasi kedua membuat akun demo lokal: `admin@demo.wbl.test` / `DemoAdmin!2026`, `staff@demo.wbl.test` / `DemoStaff!2026`, dan `reporter@demo.wbl.test` / `DemoReport!2026`. Kredensial ini hanya untuk lingkungan development.

Belum tersedia ledger penyusutan dan period locking, workflow legalitas dan perpanjangan kendaraan, pengelolaan pengguna, laporan PDF, OpenAPI, serta seluruh detail ERD/PRD. Pemindaian kamera belum diuji pada perangkat fisik. Proyek `asset-wbl` belum ditemukan pada akun Vercel yang terhubung; URL `asset-wbl.vercel.app` belum terverifikasi sebagai deployment aplikasi ini. API Go, worker, database, dan storage privat memerlukan hosting terpisah dari frontend statis Vercel.
