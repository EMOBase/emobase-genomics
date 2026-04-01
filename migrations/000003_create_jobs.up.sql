CREATE TABLE jobs (
    id              BIGINT UNSIGNED AUTO_INCREMENT                  PRIMARY KEY,
    version_id      BIGINT UNSIGNED                                 NOT NULL,
    type            VARCHAR(100)                                    NOT NULL,
    payload         JSON,
    status          ENUM('PENDING','RUNNING','DONE','FAILED')       NOT NULL DEFAULT 'PENDING',
    retry_count     INT UNSIGNED                                    NOT NULL DEFAULT 0,
    max_retry_count INT UNSIGNED                                    NOT NULL DEFAULT 3,
    result_metadata JSON,
    created_at      DATETIME                                        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME                                        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    started_at      DATETIME,
    completed_at    DATETIME,

    CONSTRAINT fk_jobs_version FOREIGN KEY (version_id) REFERENCES versions(id),
    INDEX idx_jobs_status (status),
    INDEX idx_jobs_version (version_id)
);
