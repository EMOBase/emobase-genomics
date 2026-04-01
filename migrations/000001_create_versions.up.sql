CREATE TABLE versions (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(255)                        NOT NULL,
    description TEXT,
    status      ENUM('DRAFT','PROCESSING','READY')  NOT NULL DEFAULT 'DRAFT',
    created_at  DATETIME                            NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  VARCHAR(255)                        NOT NULL,
    updated_at  DATETIME                            NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    updated_by  VARCHAR(255)                        NOT NULL
);
