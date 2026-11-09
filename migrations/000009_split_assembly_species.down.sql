ALTER TABLE assembly_versions
    DROP COLUMN species_name,
    RENAME COLUMN species_code TO species;
