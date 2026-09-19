-- Sample e-commerce schema for DBVault demos and end-to-end tests.
CREATE TABLE customers (
    id         serial PRIMARY KEY,
    name       text NOT NULL,
    email      text UNIQUE NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE orders (
    id          serial PRIMARY KEY,
    customer_id int NOT NULL REFERENCES customers(id),
    total       numeric(10, 2) NOT NULL,
    placed_at   timestamptz NOT NULL DEFAULT now()
);
CREATE SCHEMA analytics;
CREATE TABLE analytics.events (
    id      bigserial PRIMARY KEY,
    kind    text NOT NULL,
    payload jsonb NOT NULL
);
INSERT INTO customers (name, email)
SELECT 'Customer ' || g, 'customer' || g || '@example.com' FROM generate_series(1, 5000) g;
INSERT INTO orders (customer_id, total)
SELECT 1 + (g % 5000), round((random() * 500)::numeric, 2) FROM generate_series(1, 25000) g;
INSERT INTO analytics.events (kind, payload)
SELECT (ARRAY['view', 'click', 'purchase'])[1 + g % 3], jsonb_build_object('n', g) FROM generate_series(1, 20000) g;
CREATE VIEW top_customers AS
SELECT customer_id, sum(total) AS spent FROM orders GROUP BY customer_id ORDER BY spent DESC;
