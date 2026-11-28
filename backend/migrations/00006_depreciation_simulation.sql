-- +goose Up
ALTER TABLE assets ADD COLUMN depreciation_starts_on date, ADD COLUMN opening_accumulated numeric(18,2) NOT NULL DEFAULT 0 CHECK (opening_accumulated >= 0);
UPDATE assets SET depreciation_starts_on=DATE '2026-11-01' WHERE inventory_no LIKE 'SIM-%';
INSERT INTO depreciation_policies(category_id,book,version,method,life_months,residual,effective_on,simulation)
SELECT id,'commercial',1,
 CASE kind WHEN 'land' THEN 'none' ELSE 'straight_line' END,
 CASE kind WHEN 'machine' THEN 120 WHEN 'equipment' THEN 60 WHEN 'vehicle' THEN 96 WHEN 'building' THEN 240 END,
 0,DATE '2026-11-01',true FROM categories
ON CONFLICT(category_id,book,version) DO NOTHING;
-- +goose Down
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM depreciation_entries e JOIN depreciation_policies p ON p.id=e.policy_id WHERE p.version=1 AND p.effective_on=DATE '2026-11-01' AND p.simulation) THEN
  RAISE EXCEPTION 'Cannot roll back simulation policy with ledger entries';
 END IF;
END $$;
DELETE FROM depreciation_policies WHERE version=1 AND effective_on=DATE '2026-11-01' AND simulation;
ALTER TABLE assets DROP COLUMN opening_accumulated, DROP COLUMN depreciation_starts_on;
