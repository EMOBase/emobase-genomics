CREATE TABLE assembly_versions (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    version_id  BIGINT UNSIGNED                     NOT NULL,
    name        VARCHAR(255)                        NOT NULL,
    species     VARCHAR(255)                        NOT NULL,
    created_at  DATETIME                            NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  VARCHAR(255)                        NOT NULL,
    updated_at  DATETIME                            NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    updated_by  VARCHAR(255)                        NOT NULL,

    CONSTRAINT fk_assembly_versions_version FOREIGN KEY (version_id) REFERENCES versions(id),
    UNIQUE KEY uq_assembly_versions_version_species (version_id, species)
);
