ALTER TABLE assembly_versions ADD UNIQUE KEY uq_assembly_versions_version_species (version_id, species);
ALTER TABLE assembly_versions DROP INDEX idx_assembly_versions_version_id;
