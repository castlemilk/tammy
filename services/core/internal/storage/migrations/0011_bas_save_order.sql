CREATE TABLE bas_workpaper_save_order_v1 (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT CHECK (sequence > 0),
  workpaper_id TEXT NOT NULL UNIQUE REFERENCES bas_workpapers(id) ON DELETE RESTRICT
);

-- Capture the retained insertion order before VACUUM can rewrite hidden rowids.
INSERT INTO bas_workpaper_save_order_v1(workpaper_id)
SELECT id FROM bas_workpapers ORDER BY rowid;

CREATE TRIGGER bas_workpapers_capture_save_order
AFTER INSERT ON bas_workpapers BEGIN
  INSERT INTO bas_workpaper_save_order_v1(workpaper_id) VALUES (NEW.id);
END;

CREATE TRIGGER bas_workpaper_save_order_no_update
BEFORE UPDATE ON bas_workpaper_save_order_v1 BEGIN
  SELECT RAISE(ABORT, 'BAS workpaper save order is immutable');
END;

CREATE TRIGGER bas_workpaper_save_order_no_delete
BEFORE DELETE ON bas_workpaper_save_order_v1 BEGIN
  SELECT RAISE(ABORT, 'BAS workpaper save order is retained');
END;
