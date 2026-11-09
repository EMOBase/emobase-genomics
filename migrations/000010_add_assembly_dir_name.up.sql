-- dir_name is an assembly's upload folder under its version's folder. It is
-- fixed at creation from the assembly's name, so it must be unique within the
-- version: two assemblies sharing it would mix their files, and deleting one
-- would remove the other's. Existing assemblies keep the folder their files are
-- already in, named after their AssemblyID ("v{version_id}a{id}").
ALTER TABLE assembly_versions ADD COLUMN dir_name VARCHAR(255) NULL AFTER name;
UPDATE assembly_versions SET dir_name = CONCAT('v', version_id, 'a', id);
ALTER TABLE assembly_versions
    MODIFY COLUMN dir_name VARCHAR(255) NOT NULL,
    ADD UNIQUE KEY uq_assembly_versions_version_dir (version_id, dir_name);
