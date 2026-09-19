-- Sample shop data for MySQL/MariaDB demos and end-to-end tests.

-- Let the demo user create and restore into new shop* databases (restoring
-- into a new database needs CREATE on the server plus rights on the new one).
GRANT ALL PRIVILEGES ON `shop%`.* TO 'shop'@'%';
CREATE TABLE customers (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100) NOT NULL,
  email VARCHAR(200) NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE products (
  id INT AUTO_INCREMENT PRIMARY KEY,
  sku VARCHAR(40) NOT NULL UNIQUE,
  title VARCHAR(200) NOT NULL,
  price DECIMAL(10,2) NOT NULL
);
CREATE TABLE orders (
  id INT AUTO_INCREMENT PRIMARY KEY,
  customer_id INT NOT NULL,
  product_id INT NOT NULL,
  quantity INT NOT NULL,
  total DECIMAL(12,2) NOT NULL,
  FOREIGN KEY (customer_id) REFERENCES customers(id),
  FOREIGN KEY (product_id) REFERENCES products(id)
);

INSERT INTO customers (name, email)
SELECT CONCAT('Customer ', n), CONCAT('customer', n, '@example.com')
FROM (SELECT a.d + b.d * 10 + c.d * 100 + 1 AS n
      FROM (SELECT 0 d UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5 UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9) a,
           (SELECT 0 d UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5 UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9) b,
           (SELECT 0 d UNION SELECT 1 UNION SELECT 2 UNION SELECT 3 UNION SELECT 4) c) s;

INSERT INTO products (sku, title, price) VALUES
  ('SKU-001', 'Espresso beans 1kg', 24.90), ('SKU-002', 'Pour-over kettle', 59.00),
  ('SKU-003', 'Ceramic mug', 12.50), ('SKU-004', 'Burr grinder', 149.00), ('SKU-005', 'Paper filters (100)', 6.75);

INSERT INTO orders (customer_id, product_id, quantity, total)
SELECT c.id, p.id, 1 + (c.id % 3), p.price * (1 + (c.id % 3))
FROM customers c JOIN products p ON p.id = 1 + (c.id % 5);

CREATE VIEW customer_spend AS
  SELECT c.id, c.name, SUM(o.total) AS spent FROM customers c JOIN orders o ON o.customer_id = c.id GROUP BY c.id, c.name;

CREATE TRIGGER orders_total BEFORE INSERT ON orders FOR EACH ROW SET NEW.total = ROUND(NEW.total, 2);
