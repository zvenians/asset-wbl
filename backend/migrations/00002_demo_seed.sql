-- +goose Up
-- Local demo identities only. Replace before any non-demo deployment.
INSERT INTO users(email,password_hash,full_name,role) VALUES
 ('admin@demo.wbl.test',crypt('DemoAdmin!2026',gen_salt('bf',12)),'Administrator Demo','admin'),
 ('staff@demo.wbl.test',crypt('DemoStaff!2026',gen_salt('bf',12)),'Staf Aset Demo','staff'),
 ('reporter@demo.wbl.test',crypt('DemoReport!2026',gen_salt('bf',12)),'Pelapor Demo','reporter')
ON CONFLICT(email) DO NOTHING;
INSERT INTO business_units(name) VALUES ('Estate'),('Developer'),('Mall'),('E-Walk'),('Pentacity'),('Apartment'),('Ruby'),('Aero'),('CNC'),('PAM Tower') ON CONFLICT(name) DO NOTHING;
INSERT INTO categories(name,kind) VALUES ('Mesin','machine'),('Peralatan','equipment'),('Kendaraan','vehicle'),('Tanah','land'),('Bangunan','building') ON CONFLICT(name) DO NOTHING;
-- +goose Down
DELETE FROM users WHERE email IN ('admin@demo.wbl.test','staff@demo.wbl.test','reporter@demo.wbl.test');
