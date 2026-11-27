-- +goose Up
-- Synthetic inventory for functional demonstrations. Never treat as WBL records.
INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status,purchase_cost,acquired_on)
SELECT 'SIM-MSN-'||lpad(g::text,4,'0'),'Mesin utilitas simulasi '||g,c.id,u.id,
 CASE WHEN g%11=0 THEN 'perlu perbaikan' WHEN g%7=0 THEN 'cukup' ELSE 'baik' END,
 CASE WHEN g%17=0 THEN 'cadangan' ELSE 'aktif' END,(15000000+g*250000)::numeric,DATE '2021-01-01'+g
FROM generate_series(1,100) g CROSS JOIN LATERAL(SELECT id FROM categories WHERE kind='machine' LIMIT 1)c
CROSS JOIN LATERAL(SELECT id FROM business_units ORDER BY name OFFSET (g%10) LIMIT 1)u
ON CONFLICT(inventory_no) DO NOTHING;
INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status,purchase_cost,acquired_on)
SELECT 'SIM-PRL-'||lpad(g::text,4,'0'),'Peralatan operasional simulasi '||g,c.id,u.id,
 CASE WHEN g%13=0 THEN 'perlu perbaikan' ELSE 'baik' END,'aktif',(1000000+g*19000)::numeric,DATE '2022-01-01'+g
FROM generate_series(1,200) g CROSS JOIN LATERAL(SELECT id FROM categories WHERE kind='equipment' LIMIT 1)c
CROSS JOIN LATERAL(SELECT id FROM business_units ORDER BY name OFFSET (g%10) LIMIT 1)u
ON CONFLICT(inventory_no) DO NOTHING;
INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status,purchase_cost,acquired_on)
SELECT 'SIM-KDR-'||lpad(g::text,4,'0'),'Kendaraan operasional simulasi '||g,c.id,u.id,'baik','aktif',(190000000+g*3500000)::numeric,DATE '2020-01-01'+g
FROM generate_series(1,30) g CROSS JOIN LATERAL(SELECT id FROM categories WHERE kind='vehicle' LIMIT 1)c
CROSS JOIN LATERAL(SELECT id FROM business_units ORDER BY name OFFSET (g%10) LIMIT 1)u
ON CONFLICT(inventory_no) DO NOTHING;
INSERT INTO vehicles(asset_id,plate_no,vin,engine_no,annual_due,five_year_due,odometer_km)
SELECT id,'KT '||lpad(substring(inventory_no from 9)::int::text,4,'0')||' WB','SIM-VIN-'||inventory_no,'SIM-ENG-'||inventory_no,DATE '2027-01-01'+substring(inventory_no from 9)::int,DATE '2030-01-01'+substring(inventory_no from 9)::int,10000+substring(inventory_no from 9)::int*2500
FROM assets WHERE inventory_no LIKE 'SIM-KDR-%' ON CONFLICT(asset_id) DO NOTHING;
INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status,purchase_cost,acquired_on)
SELECT 'SIM-TNH-'||lpad(g::text,4,'0'),'Bidang tanah simulasi '||g,c.id,u.id,'baik','aktif',(500000000+g*25000000)::numeric,DATE '2018-01-01'+g
FROM generate_series(1,20) g CROSS JOIN LATERAL(SELECT id FROM categories WHERE kind='land' LIMIT 1)c
CROSS JOIN LATERAL(SELECT id FROM business_units ORDER BY name OFFSET (g%10) LIMIT 1)u
ON CONFLICT(inventory_no) DO NOTHING;
INSERT INTO lands(asset_id,area_m2,title_type,title_no,title_expiry)
SELECT id,1000+substring(inventory_no from 9)::int*50,'HGB','SIM-HGB-'||inventory_no,DATE '2035-01-01'+substring(inventory_no from 9)::int FROM assets WHERE inventory_no LIKE 'SIM-TNH-%' ON CONFLICT(asset_id) DO NOTHING;
INSERT INTO assets(inventory_no,name,category_id,unit_id,condition,usage_status,purchase_cost,acquired_on)
SELECT 'SIM-BNG-'||lpad(g::text,4,'0'),'Bangunan operasional simulasi '||g,c.id,u.id,'baik','aktif',(1500000000::bigint+g*65000000::bigint)::numeric,DATE '2019-01-01'+g
FROM generate_series(1,15) g CROSS JOIN LATERAL(SELECT id FROM categories WHERE kind='building' LIMIT 1)c
CROSS JOIN LATERAL(SELECT id FROM business_units ORDER BY name OFFSET (g%10) LIMIT 1)u
ON CONFLICT(inventory_no) DO NOTHING;
INSERT INTO buildings(asset_id,land_asset_id,area_m2,floors)
SELECT b.id,l.id,500+substring(b.inventory_no from 9)::int*40,2+substring(b.inventory_no from 9)::int%8 FROM assets b JOIN assets l ON l.inventory_no='SIM-TNH-'||lpad((1+substring(b.inventory_no from 9)::int%20)::text,4,'0') WHERE b.inventory_no LIKE 'SIM-BNG-%' ON CONFLICT(asset_id) DO NOTHING;
INSERT INTO maintenance_plans(asset_id,title,interval_days,next_due,checklist)
SELECT id,'Inspeksi berkala',90,CURRENT_DATE+(substring(inventory_no from 9)::int%45), '["Pemeriksaan visual","Catat hasil"]'::jsonb FROM assets WHERE inventory_no LIKE 'SIM-MSN-%' AND substring(inventory_no from 9)::int<=50
AND NOT EXISTS(SELECT 1 FROM maintenance_plans p WHERE p.asset_id=assets.id);
INSERT INTO damage_reports(reporter_id,asset_id,location_text,description,urgency,status)
SELECT u.id,a.id,'Area operasional simulasi','Kerusakan simulasi untuk pengujian alur kerja nomor '||g,CASE WHEN g%3=0 THEN 'tinggi' ELSE 'sedang' END,'baru'
FROM generate_series(1,30)g JOIN users u ON u.email='reporter@demo.wbl.test' JOIN assets a ON a.inventory_no='SIM-PRL-'||lpad(g::text,4,'0')
WHERE NOT EXISTS(SELECT 1 FROM damage_reports d WHERE d.asset_id=a.id AND d.description LIKE 'Kerusakan simulasi%');
-- +goose Down
DELETE FROM damage_reports WHERE description LIKE 'Kerusakan simulasi untuk pengujian%';
DELETE FROM maintenance_plans WHERE title='Inspeksi berkala' AND asset_id IN(SELECT id FROM assets WHERE inventory_no LIKE 'SIM-MSN-%');
DELETE FROM buildings WHERE asset_id IN(SELECT id FROM assets WHERE inventory_no LIKE 'SIM-BNG-%');
DELETE FROM lands WHERE asset_id IN(SELECT id FROM assets WHERE inventory_no LIKE 'SIM-TNH-%');
DELETE FROM vehicles WHERE asset_id IN(SELECT id FROM assets WHERE inventory_no LIKE 'SIM-KDR-%');
DELETE FROM assets WHERE inventory_no LIKE 'SIM-%';
