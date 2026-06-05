-- Multi-warehouse fulfilment: record which warehouse a shipment ships from, so
-- stock is decremented from the right location (FulfilShipment) rather than
-- always the default warehouse. Nullable — existing shipments + the default
-- single-warehouse path are unaffected.
ALTER TABLE shipments ADD COLUMN warehouse_id BIGINT REFERENCES warehouses(id);
