-- +goose Up
INSERT INTO locations(name,unit_id)
SELECT 'Area simulasi '||u.name,u.id FROM business_units u
WHERE NOT EXISTS(SELECT 1 FROM locations l WHERE l.unit_id=u.id AND l.name='Area simulasi '||u.name);
INSERT INTO locations(name,parent_id,unit_id)
SELECT 'Lantai '||g||' simulasi '||u.name,l.id,u.id
FROM business_units u JOIN locations l ON l.unit_id=u.id AND l.parent_id IS NULL AND l.name='Area simulasi '||u.name CROSS JOIN generate_series(1,3)g
WHERE NOT EXISTS(SELECT 1 FROM locations x WHERE x.parent_id=l.id AND x.name='Lantai '||g||' simulasi '||u.name);
UPDATE assets a SET location_id=l.id FROM locations l WHERE a.location_id IS NULL AND l.unit_id=a.unit_id AND l.parent_id IS NULL AND a.inventory_no LIKE 'SIM-%';
-- +goose Down
UPDATE assets SET location_id=NULL WHERE inventory_no LIKE 'SIM-%';
DELETE FROM locations WHERE name LIKE 'Lantai % simulasi %';
DELETE FROM locations WHERE name LIKE 'Area simulasi %';
