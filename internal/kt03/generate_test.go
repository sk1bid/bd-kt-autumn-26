package kt03

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratorReproducibleAndSeedMatters(t *testing.T) {
	r := &runner{ctx: context.Background(), mode: "dev", seed: 42, rows: 20000, data: t.TempDir(), out: t.TempDir()}
	if err := r.generate(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.generate(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("identical seed changed dataset")
	}
	originalManifest, err := r.readManifest()
	if err != nil {
		t.Fatal(err)
	}
	r.seed++
	if err = r.generate(); err != nil {
		t.Fatal(err)
	}
	after, err = os.ReadFile(filepath.Join(r.data, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		t.Fatal("different seed did not change dataset")
	}
	changedManifest, err := r.readManifest()
	if err != nil {
		t.Fatal(err)
	}
	if originalManifest.Tables[18].SHA256 == changedManifest.Tables[18].SHA256 {
		t.Fatal("different seed did not change message rows")
	}
	f, err := os.OpenFile(filepath.Join(r.data, "message_key.csv"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("corruption\n")
	f.Close()
	if _, err = r.readManifest(); err == nil {
		t.Fatal("corrupted data accepted")
	}
}

func TestMedian(t *testing.T) {
	if median([]float64{10, 1, 2}) != 2 || median([]float64{1, 3, 8, 2}) != 2.5 {
		t.Fatal("incorrect median")
	}
}
