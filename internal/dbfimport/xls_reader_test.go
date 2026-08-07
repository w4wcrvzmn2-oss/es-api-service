package dbfimport

import "testing"

func TestDetectXLSFormat(t *testing.T) {
	if DetectDataFileFormat("price.xls") != "xls" {
		t.Fatal("xls not detected")
	}
	if !IsSupportedDataFile("PRICE.XLS") {
		t.Fatal("xls must be supported")
	}
}

func TestReadXLSSample(t *testing.T) {
	headers, records, err := ReadTabularFile("testdata/sample.xls")
	if err != nil {
		t.Fatalf("read xls: %v", err)
	}
	if len(headers) == 0 {
		t.Fatalf("no headers parsed from .xls")
	}
	if len(records) == 0 {
		t.Fatalf("no records parsed from .xls")
	}
	t.Logf("xls parsed: headers=%v records=%d", headers, len(records))
}
