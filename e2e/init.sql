USE shop;

CREATE TABLE orders (
    id BIGINT NOT NULL PRIMARY KEY,
    note VARCHAR(64) NOT NULL,
    create_time DATETIME NOT NULL
);

INSERT INTO orders (id, note, create_time) VALUES
    (1, 'old-a', '2024-06-01 00:00:00'),
    (2, 'old-b', '2024-11-01 00:00:00'),
    (3, 'keep', '2025-06-01 00:00:00');
