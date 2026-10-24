-- A species code does not identify an assembly, so one Database Version may hold
-- several assemblies of the same species. The foreign key on version_id still
-- needs an index, so add a plain one before dropping the unique key.
ALTER TABLE assembly_versions ADD INDEX idx_assembly_versions_version_id (version_id);
ALTER TABLE assembly_versions DROP INDEX uq_assembly_versions_version_species;
