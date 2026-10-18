package db

import "testing"

func TestMigrationURLDropsPoolSettings(t *testing.T) {
	got := migrationURL("postgres://u:p@h:5432/db?sslmode=disable&pool_max_conns=2&pool_min_conns=0")
	if got != "postgres://u:p@h:5432/db?sslmode=disable" {
		t.Errorf("got %q", got)
	}
	if got := migrationURL("postgres://u:p@h/db"); got != "postgres://u:p@h/db" {
		t.Errorf("got %q", got)
	}
}
