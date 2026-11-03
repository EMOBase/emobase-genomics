ALTER TABLE assembly_versions
    ADD COLUMN image_path VARCHAR(1024) NULL AFTER species,
    ADD COLUMN info_url   VARCHAR(2048) NULL AFTER image_path;
