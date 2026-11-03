ALTER TABLE upload_files
    ADD COLUMN assembly_version_id BIGINT UNSIGNED NULL AFTER version_id,
    ADD CONSTRAINT fk_upload_files_assembly_version
        FOREIGN KEY (assembly_version_id) REFERENCES assembly_versions(id);

ALTER TABLE jobs
    ADD COLUMN assembly_version_id BIGINT UNSIGNED NULL AFTER version_id,
    ADD CONSTRAINT fk_jobs_assembly_version
        FOREIGN KEY (assembly_version_id) REFERENCES assembly_versions(id),
    ADD INDEX idx_jobs_assembly_version (assembly_version_id);
