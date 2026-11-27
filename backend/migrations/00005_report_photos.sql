-- +goose Up
CREATE TABLE report_photos (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 report_id uuid NOT NULL REFERENCES damage_reports(id) ON DELETE CASCADE,
 storage_key text NOT NULL UNIQUE,
 filename text NOT NULL,
 mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg','image/png')),
 size_bytes bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 8388608),
 uploaded_by uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX report_photos_report_idx ON report_photos(report_id);
-- +goose Down
DROP TABLE report_photos;
