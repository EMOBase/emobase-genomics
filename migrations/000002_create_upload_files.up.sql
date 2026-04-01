CREATE TABLE upload_files (
    id            VARCHAR(36)                               PRIMARY KEY,
    version_id    BIGINT UNSIGNED                           NOT NULL,
    file_path     VARCHAR(1024)                             NOT NULL,
    file_type     VARCHAR(100)                              NOT NULL,
    metadata      JSON,
    upload_status ENUM('UPLOADING','COMPLETED','FAILED')    NOT NULL DEFAULT 'UPLOADING',
    created_at    DATETIME                                  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by    VARCHAR(255)                              NOT NULL,
    completed_at  DATETIME,
    deleted_at    DATETIME,
    deleted_by    VARCHAR(255),

    CONSTRAINT fk_upload_files_version FOREIGN KEY (version_id) REFERENCES versions(id)
);
