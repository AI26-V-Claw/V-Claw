package main

import "testing"

func TestParseRowsJSON(t *testing.T) {
	rows, err := parseRowsJSON(`[["Name","Value"],["Smoke",1]]`)
	if err != nil {
		t.Fatalf("parseRowsJSON: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "Name" || rows[1][1].(float64) != 1 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestParseRangesJSON(t *testing.T) {
	ranges, err := parseRangesJSON(`{"Sheet1!A1:B1":[["Name","Value"]]}`)
	if err != nil {
		t.Fatalf("parseRangesJSON: %v", err)
	}
	if len(ranges["Sheet1!A1:B1"]) != 1 || ranges["Sheet1!A1:B1"][0][1] != "Value" {
		t.Fatalf("unexpected ranges: %#v", ranges)
	}
}
