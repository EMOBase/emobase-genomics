CREATE TABLE app_settings (
    id                 INT UNSIGNED    AUTO_INCREMENT PRIMARY KEY,
    default_version_id BIGINT UNSIGNED,

    CONSTRAINT fk_app_settings_version FOREIGN KEY (default_version_id) REFERENCES versions(id) ON DELETE SET NULL
);

-- Seed the single settings row
INSERT INTO app_settings (default_version_id) VALUES (NULL);
