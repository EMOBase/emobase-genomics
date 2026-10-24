-- species becomes species_code (still the short code copied into indexed ES
-- documents), and the admin now enters the full species_name it is derived
-- from. A code cannot be expanded back into a name, so existing rows get their
-- code as a placeholder name.
ALTER TABLE assembly_versions
    RENAME COLUMN species TO species_code,
    ADD COLUMN species_name VARCHAR(255) NULL AFTER name;
UPDATE assembly_versions SET species_name = species_code;
ALTER TABLE assembly_versions MODIFY COLUMN species_name VARCHAR(255) NOT NULL;
