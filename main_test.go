package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"math"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestAverage(t *testing.T) {
	tests := []struct {
		name string
		nums []float64
		want float64
	}{
		{"single value", []float64{5.0}, 5.0},
		{"two values", []float64{2.0, 4.0}, 3.0},
		{"multiple values", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 3.0},
		{"identical values", []float64{7.0, 7.0, 7.0}, 7.0},
		{"decimals", []float64{1.5, 2.5}, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := average(tt.nums)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("average(%v) = %v, want %v", tt.nums, got, tt.want)
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		sorted []float64
		p      int
		want   float64
	}{
		{"empty slice", []float64{}, 95, 0},
		{"single value p50", []float64{10.0}, 50, 10.0},
		{"single value p99", []float64{10.0}, 99, 10.0},
		{"ten values p95", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 95, 10},
		{"ten values p99", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 99, 10},
		{"ten values p50", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 50, 5},
		{"hundred values p95", makeRange(1, 100), 95, 95},
		{"hundred values p99", makeRange(1, 100), 99, 99},
		{"hundred values p50", makeRange(1, 100), 50, 50},
		{"p100", []float64{1, 2, 3, 4, 5}, 100, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.sorted, tt.p)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("percentile(%v, %d) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
}

func makeRange(min, max int) []float64 {
	r := make([]float64, max-min+1)
	for i := range r {
		r[i] = float64(min + i)
	}
	return r
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want string
	}{
		{"full 40-char hash", "abc1234567890def1234567890abcdef12345678", "abc1234"},
		{"empty string", "", ""},
		{"default commit", "0000000", "0000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortCommit(tt.hash)
			if got != tt.want {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.hash, got, tt.want)
			}
		})
	}
}

func TestShortDate(t *testing.T) {
	tests := []struct {
		name string
		date string
		want string
	}{
		{"RFC3339 full", "2026-03-08T22:08:22Z", "2026-03-08"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortDate(tt.date)
			if got != tt.want {
				t.Errorf("shortDate(%q) = %q, want %q", tt.date, got, tt.want)
			}
		})
	}
}

func setupTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "qbench-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to open sqlite3: %v", err)
	}

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL)")
	if err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("failed to create table: %v", err)
	}

	for i := 1; i <= 10; i++ {
		_, err = db.Exec("INSERT INTO test (id, name, value) VALUES (?, ?, ?)", i, fmt.Sprintf("item%d", i), float64(i)*1.5)
		if err != nil {
			db.Close()
			os.Remove(tmpFile.Name())
			t.Fatalf("failed to insert row: %v", err)
		}
	}

	return db, tmpFile.Name()
}

func TestOpenDB(t *testing.T) {
	db, path := setupTestDB(t)
	db.Close()
	defer os.Remove(path)

	options := execOptions{
		driver:      "sqlite3",
		dsn:         path,
		concurrency: 1,
	}
	opened := openDB(options)
	defer opened.Close()

	err := opened.Ping()
	if err != nil {
		t.Fatalf("openDB returned a db that can't ping: %v", err)
	}
}

func TestRunWarmupZero(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	runWarmup(db, "SELECT 1", 0, 1)
}

func TestRunWarmupExecutesQueries(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	runWarmup(db, "SELECT * FROM test", 20, 4)
}

func TestRunBenchmarkSelectOne(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	options := execOptions{
		query:         "SELECT 1",
		totalRequests: 50,
		concurrency:   2,
	}

	results := runBenchmark(db, options)

	if results.completeReqs != 50 {
		t.Errorf("completeReqs = %d, want 50", results.completeReqs)
	}
	if results.failedReqs != 0 {
		t.Errorf("failedReqs = %d, want 0", results.failedReqs)
	}
	if results.totalTime <= 0 {
		t.Errorf("totalTime = %f, want > 0", results.totalTime)
	}
	if results.rps <= 0 {
		t.Errorf("rps = %f, want > 0", results.rps)
	}
	if results.fetchedRows != 50 {
		t.Errorf("fetchedRows = %d, want 50", results.fetchedRows)
	}
	if results.avgLatency <= 0 {
		t.Errorf("avgLatency = %f, want > 0", results.avgLatency)
	}
	if results.p95Latency <= 0 {
		t.Errorf("p95Latency = %f, want > 0", results.p95Latency)
	}
	if results.p99Latency <= 0 {
		t.Errorf("p99Latency = %f, want > 0", results.p99Latency)
	}
}

func TestRunBenchmarkMultipleRows(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	options := execOptions{
		query:         "SELECT id, name, value FROM test",
		totalRequests: 10,
		concurrency:   2,
	}

	results := runBenchmark(db, options)

	if results.completeReqs != 10 {
		t.Errorf("completeReqs = %d, want 10", results.completeReqs)
	}
	if results.failedReqs != 0 {
		t.Errorf("failedReqs = %d, want 0", results.failedReqs)
	}
	if results.fetchedRows != 100 {
		t.Errorf("fetchedRows = %d, want 100", results.fetchedRows)
	}
	if results.rowsPerQuery != 10.0 {
		t.Errorf("rowsPerQuery = %f, want 10", results.rowsPerQuery)
	}
	if results.fetchedBytes <= 0 {
		t.Errorf("fetchedBytes = %d, want > 0", results.fetchedBytes)
	}
	if results.bytesPerQuery <= 0 {
		t.Errorf("bytesPerQuery = %f, want > 0", results.bytesPerQuery)
	}
}

func TestRunBenchmarkInvalidQuery(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	options := execOptions{
		query:         "SELECT * FROM nonexistent_table",
		totalRequests: 5,
		concurrency:   1,
	}

	results := runBenchmark(db, options)

	if results.failedReqs != 5 {
		t.Errorf("failedReqs = %d, want 5", results.failedReqs)
	}
	if results.completeReqs != 0 {
		t.Errorf("completeReqs = %d, want 0", results.completeReqs)
	}
}

func TestRunBenchmarkConcurrency(t *testing.T) {
	db, path := setupTestDB(t)
	defer db.Close()
	defer os.Remove(path)

	for _, c := range []int{1, 4, 8} {
		t.Run(fmt.Sprintf("concurrency_%d", c), func(t *testing.T) {
			options := execOptions{
				query:         "SELECT 1",
				totalRequests: 20,
				concurrency:   c,
			}

			results := runBenchmark(db, options)

			if results.completeReqs != 20 {
				t.Errorf("concurrency %d: completeReqs = %d, want 20", c, results.completeReqs)
			}
			if results.failedReqs != 0 {
				t.Errorf("concurrency %d: failedReqs = %d, want 0", c, results.failedReqs)
			}
		})
	}
}

func TestPrintConnectionInfoMySQL(t *testing.T) {
	options := execOptions{
		driver: "mysql",
		user:   "testuser",
		host:   "localhost",
		port:   3306,
		dbname: "testdb",
		query:  "SELECT 1",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printConnectionInfo(options)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "Connection: testuser@localhost:3306/testdb\nQuery: SELECT 1\n\n"
	if output != expected {
		t.Errorf("printConnectionInfo output:\n%q\nwant:\n%q", output, expected)
	}
}

func TestPrintConnectionInfoSQLite(t *testing.T) {
	options := execOptions{
		driver: "sqlite3",
		dbname: "/tmp/test.db",
		query:  "SELECT 1",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printConnectionInfo(options)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "Connection: sqlite3:///tmp/test.db\nQuery: SELECT 1\n\n"
	if output != expected {
		t.Errorf("printConnectionInfo output:\n%q\nwant:\n%q", output, expected)
	}
}

func TestPrintResults(t *testing.T) {
	r := benchmarkResults{
		totalTime:     1.23,
		completeReqs:  100,
		failedReqs:    0,
		rps:           81.30,
		fetchedRows:   1000,
		rowsPerQuery:  10,
		fetchedBytes:  5000,
		bytesPerQuery: 50,
		avgLatency:    12.34,
		p95Latency:    20.00,
		p99Latency:    25.00,
	}

	old := os.Stdout
	rd, w, _ := os.Pipe()
	os.Stdout = w

	printResults(r)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(rd)
	output := buf.String()

	checks := []string{
		"Benchmark Results",
		"Total time:        1.230 sec",
		"Complete requests: 100",
		"Failed requests:   0",
		"Requests/sec:      81.30",
		"Fetched rows:      1000",
		"Rows/query:        10",
		"Fetched data:      5000 bytes",
		"Bytes/query:       50",
		"Average latency:   12.34 ms",
		"P95 latency:       20.00 ms",
		"P99 latency:       25.00 ms",
	}

	for _, check := range checks {
		if !bytes.Contains([]byte(output), []byte(check)) {
			t.Errorf("printResults output missing %q", check)
		}
	}
}
