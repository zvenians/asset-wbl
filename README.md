# WBL Asset Management Phase 01

Monorepo demo operasional WBL. **Implementasi Phase 01 belum lengkap dan belum layak produksi.** Seluruh identitas dan data seed adalah simulasi.

Tersedia API Go/Chi untuk login, aset, mutasi, peta lokasi berbasis hierarki yang tersedia, import/export Excel dengan review duplikat, laporan kerusakan dan foto privat, work order, dokumen privat, notifikasi, ledger penyusutan simulasi, riwayat perpanjangan kendaraan dan pembaruan jatuh tempo, pembuatan akun dan perubahan peran oleh admin, serta pembacaan audit trail oleh admin. Perubahan peran mengakhiri sesi aktif pengguna terkait. Workspace React memakai desain editorial netral/indigo, font Geist, ikon Lucide, logo resmi WBL, foto resmi BSB/PAM Tower, dashboard dari data database, label QR aset, dan command palette. Portal Vue memakai desain yang sama serta pemindaian QR kamera pada browser yang mendukung BarcodeDetector. Worker menangani preventive maintenance dan email SMTP. Migrasi demo mengisi 365 aset, 50 rencana preventive, dan 30 laporan simulasi.

## Menjalankan lokal

Prasyarat: Go 1.26+, PostgreSQL 17, pnpm, dan Goose.

1. Salin `.env.example` ke `.env` dan sesuaikan. Jangan commit `.env`.
2. Jalankan `docker compose up -d`.
3. Jalankan migrasi dengan `goose -dir backend/migrations postgres "$DATABASE_URL" up`.
4. Jalankan `cd backend && go run ./cmd/api` dan `cd backend && go run ./cmd/worker` pada terminal terpisah. Pastikan variabel lingkungan dari `.env` sudah diekspor ke proses; PowerShell tidak memuatnya otomatis.
5. Jalankan `pnpm install`, `pnpm dev:workspace`, dan `pnpm dev:portal` pada terminal terpisah.

Mailpit dapat dibuka di `http://localhost:8025`. Pada demo, email hanya dikirim ke `DEMO_EMAIL_ALLOWLIST`. Uji build dengan `pnpm build` dan `cd backend && go test ./...`.

Migrasi kedua membuat akun demo lokal: `admin@demo.wbl.test` / `DemoAdmin!2026`, `staff@demo.wbl.test` / `DemoStaff!2026`, dan `reporter@demo.wbl.test` / `DemoReport!2026`. Kredensial ini hanya untuk lingkungan development.

Ledger penyusutan dan period locking tersedia sebagai **simulasi**, bukan kebijakan Accounting WBL. Belum tersedia workflow legalitas properti, unggah STNK/BPKB yang terkait langsung ke kendaraan, penonaktifan akun dan reset kata sandi melalui UI, laporan PDF, OpenAPI, serta seluruh detail ERD/PRD. Pemindaian kamera belum diuji pada perangkat fisik. Logo dan foto resmi tercatat beserta sumbernya di `ASSETS.md`; hak publikasi perlu dipastikan sebelum rilis publik. Proyek `asset-wbl` belum ditemukan pada akun Vercel yang terhubung; URL `asset-wbl.vercel.app` belum terverifikasi sebagai deployment aplikasi ini. API Go, worker, database, dan storage privat memerlukan hosting terpisah dari frontend statis Vercel.
