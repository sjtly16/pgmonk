-- Demo dataset for pg-monk screenshots / recordings.
-- Creates a small "shop" schema with deliberately unhealthy tables so the
-- analyzer has something to say.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

DROP TABLE IF EXISTS order_items, orders, customers, products, events CASCADE;

CREATE TABLE customers (
  id         bigserial PRIMARY KEY,
  email      text NOT NULL,
  country    text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE products (
  id    bigserial PRIMARY KEY,
  sku   text NOT NULL,
  name  text NOT NULL,
  price numeric(10,2) NOT NULL,
  stock int NOT NULL
);

CREATE TABLE orders (
  id          bigserial PRIMARY KEY,
  customer_id bigint NOT NULL REFERENCES customers(id),
  status      text NOT NULL,
  total       numeric(12,2) NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
  order_id   bigint NOT NULL,
  product_id bigint NOT NULL,
  qty        int NOT NULL
);

-- Append-only event log; autovacuum disabled so dead tuples pile up.
CREATE TABLE events (
  id      bigserial PRIMARY KEY,
  kind    text NOT NULL,
  payload text NOT NULL,
  seen    boolean NOT NULL DEFAULT false
) WITH (autovacuum_enabled = false);

INSERT INTO customers (email, country, created_at)
SELECT 'user' || g || '@example.com',
       (ARRAY['IN','US','DE','BR','JP'])[1 + g % 5],
       now() - (g || ' minutes')::interval
FROM generate_series(1, 50000) g;

INSERT INTO products (sku, name, price, stock)
SELECT 'SKU-' || g, 'Product ' || g, (random() * 200)::numeric(10,2), (random() * 500)::int
FROM generate_series(1, 5000) g;

INSERT INTO orders (customer_id, status, total, created_at)
SELECT 1 + (random() * 49999)::int,
       (ARRAY['pending','paid','shipped','cancelled'])[1 + (random() * 3)::int],
       (random() * 900)::numeric(12,2),
       now() - (random() * 90 || ' days')::interval
FROM generate_series(1, 200000) g;

-- No index on order_items.order_id: lookups will seq scan.
INSERT INTO order_items (order_id, product_id, qty)
SELECT 1 + (random() * 199999)::int, 1 + (random() * 4999)::int, 1 + (random() * 4)::int
FROM generate_series(1, 300000) g;

INSERT INTO events (kind, payload)
SELECT (ARRAY['click','view','cart','checkout'])[1 + g % 4], repeat('x', 200)
FROM generate_series(1, 200000) g;

-- Churn the event log to create lots of dead tuples.
UPDATE events SET seen = true WHERE id % 2 = 0;
DELETE FROM events WHERE id % 3 = 0;

ANALYZE customers, products, orders;
