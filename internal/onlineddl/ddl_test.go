package onlineddl

import "testing"

func TestParseAlterAndProgress(t *testing.T) {
	db, table, body, err := ParseAlter("ALTER TABLE shop.orders ADD COLUMN note varchar(32)")
	if err != nil {
		t.Fatal(err)
	}
	if db != "shop" || table != "orders" || body != "ADD COLUMN note varchar(32)" {
		t.Fatalf("%s %s %s", db, table, body)
	}
	progress, ok := ParseLine("Copy: 1200/8000 15.0%; Applied: 3; Backlog: 0/1000; Time: 4s(total), 3s(copy); streamer: mysql-bin.0001:10; ETA: 21s")
	if !ok || progress.Copied != 1200 || progress.Total != 8000 || progress.Percent != 15 || progress.ETA != "21s" {
		t.Fatalf("%+v %v", progress, ok)
	}
	if _, _, _, err := ParseAlter("ALTER TABLE orders ADD COLUMN a int; DROP TABLE orders"); err == nil {
		t.Fatal("two statements were accepted")
	}
}
