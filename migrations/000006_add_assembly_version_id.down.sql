ALTER TABLE jobs
    DROP FOREIGN KEY fk_jobs_assembly_version,
    DROP INDEX idx_jobs_assembly_version,
    DROP COLUMN assembly_version_id;

ALTER TABLE upload_files
    DROP FOREIGN KEY fk_upload_files_assembly_version,
    DROP COLUMN assembly_version_id;
