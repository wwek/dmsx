package sqlident

import "testing"

func TestParsePredicateAcceptsColumnComparisons(t *testing.T) {
	cases := []struct {
		text  string
		mysql string
		ansi  string
	}{
		{"create_time < '2025-01-01'", "`create_time` < '2025-01-01'", `"create_time" < '2025-01-01'`},
		{"id = 1", "`id` = 1", `"id" = 1`},
		{"id != 1", "`id` != 1", `"id" != 1`},
		{"id <> 1", "`id` <> 1", `"id" <> 1`},
		{"id <= 10", "`id` <= 10", `"id" <= 10`},
		{"id >= 10", "`id` >= 10", `"id" >= 10`},
		{"id > 10", "`id` > 10", `"id" > 10`},
		{"id < 10", "`id` < 10", `"id" < 10`},
		{"note = 'old'", "`note` = 'old'", `"note" = 'old'`},
		{"amount > 1.5", "`amount` > 1.5", `"amount" > 1.5`},
		{"amount > -2", "`amount` > -2", `"amount" > -2`},
		{"flag = TRUE", "`flag` = TRUE", `"flag" = TRUE`},
		{"note IS NULL", "`note` IS NULL", `"note" IS NULL`},
		{"note IS NOT NULL", "`note` IS NOT NULL", `"note" IS NOT NULL`},
		{"note LIKE 'old%'", "`note` LIKE 'old%'", `"note" LIKE 'old%'`},
		{"id IN (1, 2, 3)", "`id` IN (1, 2, 3)", `"id" IN (1, 2, 3)`},
		{"kind IN ('a', 'b')", "`kind` IN ('a', 'b')", `"kind" IN ('a', 'b')`},
		{"id = 1 AND kind = 'even'", "(`id` = 1 AND `kind` = 'even')", `("id" = 1 AND "kind" = 'even')`},
		{"id = 1 OR id = 2", "(`id` = 1 OR `id` = 2)", `("id" = 1 OR "id" = 2)`},
		{"NOT (id = 1)", "(NOT (`id` = 1))", `(NOT ("id" = 1))`},
		{"(id = 1 OR id = 2) AND kind = 'even'", "((`id` = 1 OR `id` = 2) AND `kind` = 'even')", `(("id" = 1 OR "id" = 2) AND "kind" = 'even')`},
		{"note = 'it''s'", "`note` = 'it''s'", `"note" = 'it''s'`},
	}
	for _, c := range cases {
		predicate, err := ParsePredicate(c.text)
		if err != nil {
			t.Fatalf("%q: %v", c.text, err)
		}
		if got := predicate.MySQL(); got != c.mysql {
			t.Fatalf("%q mysql %q, want %q", c.text, got, c.mysql)
		}
		if got := predicate.ANSI(); got != c.ansi {
			t.Fatalf("%q ansi %q, want %q", c.text, got, c.ansi)
		}
	}
}

func TestParsePredicateRejectsAnythingElse(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"note = (SELECT note FROM parquet_scan('/etc/passwd'))",
		"note = (select 1)",
		"parquet_scan('/etc/passwd')",
		"parquet_scan('/etc/passwd').filename",
		"duckdb_settings()",
		"current_setting('s3_secret_access_key')",
		"read_parquet('/etc/passwd')",
		"id = 1; DROP TABLE orders",
		"id = 1 UNION SELECT 1",
		"1 = 1",
		"id = other_column",
		"id IN (SELECT id FROM orders)",
		"id IN ()",
		"id IN (1,",
		"id",
		"id =",
		"= 1",
		"note = 'unclosed",
		"id = ?",
		"id = :id",
		"id = $1",
		"id = @id",
		"table = 'orders'",
		"select = 1",
		"note = 'a' AND",
		"note LIKE 5",
		"note LIKE other_column",
		"id = 1 AND (id = 2",
	}
	for _, text := range cases {
		if _, err := ParsePredicate(text); err == nil {
			t.Fatalf("%q was accepted", text)
		}
	}
}

// The filter of an archive must not be able to reach the file system of the
// server, and the filter of a cold query must not be able to read a file
// outside the archive.
func TestParsePredicateKeepsTheOperatorTextOut(t *testing.T) {
	texts := []string{
		"note = (SELECT note FROM parquet_scan('/etc/passwd'))",
		"current_setting('s3_secret_access_key') = 'x'",
		"duckdb_settings() = 'x'",
	}
	for _, text := range texts {
		predicate, err := ParsePredicate(text)
		if err == nil {
			t.Fatalf("%q was accepted", text)
		}
		if predicate.root != nil {
			t.Fatalf("%q produced a predicate", text)
		}
	}
}

func TestParsePredicateIgnoresCaseAndSpacing(t *testing.T) {
	predicate, err := ParsePredicate("ID   =   1   and   Kind='even'")
	if err != nil {
		t.Fatal(err)
	}
	if got := predicate.ANSI(); got != `("ID" = 1 AND "Kind" = 'even')` {
		t.Fatalf("ansi %q", got)
	}
}

// A tab or a newline used to walk past the word list of the old filter
// check. The parser reads whitespace as whitespace.
func TestParsePredicateDoesNotAcceptBrokenWords(t *testing.T) {
	cases := []string{
		"id = 1\tset x = 1",
		"id = 1\nset x = 1",
		"id = 1\r\nset x = 1",
		"id = 1\tunion select 1",
	}
	for _, text := range cases {
		if _, err := ParsePredicate(text); err == nil {
			t.Fatalf("%q was accepted", text)
		}
	}
}
