ALTER TABLE assembly_versions
    DROP INDEX uq_assembly_versions_version_dir,
    DROP COLUMN dir_name;
