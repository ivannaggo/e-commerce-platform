CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price BIGINT NOT NULL CHECK (price >= 0),
    stock INTEGER NOT NULL CHECK (stock >= 0),
    -- reserved_stock is reserved for future inventory reservation flows (e.g. checkout hold).
    -- Currently not modified by this service; defaults to 0.
    reserved_stock INTEGER NOT NULL DEFAULT 0 CHECK (reserved_stock >= 0 AND reserved_stock <= stock),
    currency CHAR(3) NOT NULL,
    -- status values map to proto enum:
    --   1 = PRODUCT_STATUS_ACTIVE
    --   2 = PRODUCT_STATUS_OUT_OF_STOCK
    --   3 = PRODUCT_STATUS_DISABLED
    status INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    -- idempotency_key is stored directly on the product row to prevent race conditions.
    -- When a create/update/delete request carries an idempotency_key, it is written here.
    -- A partial unique index ensures no two operations can claim the same key,
    -- and the check-then-act pattern is replaced by a single unique-constraint guard.
    idempotency_key TEXT,
    CONSTRAINT products_status_check
        CHECK (
            status IN (1, 2, 3)
            AND (
                (status = 1 AND stock > 0)
                OR (status = 2 AND stock = 0)
                OR (status = 3)
            )
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_products_idempotency_key
    ON products (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_products_created_at
    ON products (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_products_status_created_at
    ON products (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_products_currency_created_at
    ON products (currency, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_products_price_created_at
    ON products (price, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_products_stock_status
    ON products (stock, status);

-- product_operation_idempotency is retained as a secondary audit trail.
-- The authoritative guard for idempotency is now the unique index on products.idempotency_key.
CREATE TABLE IF NOT EXISTS product_operation_idempotency (
    operation_type TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (operation_type, idempotency_key),
    CONSTRAINT product_operation_idempotency_operation_type_check
        CHECK (operation_type IN ('create', 'update', 'delete'))
);

CREATE INDEX IF NOT EXISTS idx_product_operation_idempotency_product_id
    ON product_operation_idempotency (product_id);
