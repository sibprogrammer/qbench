package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/pflag"
)

var (
	commit = "000000"
	date   = ""
)

//go:embed version
var version string

type execOptions struct {
	driver        string
	dsn           string
	host          string
	port          int
	user          string
	dbname        string
	query         string
	totalRequests int
	concurrency   int
	warmup        int
}

type benchmarkResults struct {
	totalTime     float64
	completeReqs  int
	failedReqs    int64
	rps           float64
	fetchedRows   int64
	rowsPerQuery  float64
	fetchedBytes  int64
	bytesPerQuery float64
	avgLatency    float64
	p95Latency    float64
	p99Latency    float64
}

func parseFlags() execOptions {
	showVersion := pflag.BoolP("version", "v", false, "Print version and exit")
	host := pflag.StringP("host", "h", "127.0.0.1", "MySQL host")
	port := pflag.IntP("port", "P", 3306, "MySQL port")
	user := pflag.StringP("user", "u", "root", "MySQL user")
	password := pflag.StringP("password", "p", "", "MySQL password")
	dbname := pflag.StringP("database", "d", "", "Database name (or file path for SQLite)")
	query := pflag.StringP("execute", "e", "SELECT 1", "SQL query")
	totalRequests := pflag.IntP("requests", "n", 1000, "Total number of requests")
	concurrency := pflag.IntP("concurrency", "c", 1, "Concurrency level")
	warmup := pflag.IntP("warmup", "w", 0, "Number of warmup queries before benchmarking (default 0)")
	pflag.Parse()

	if *showVersion {
		fullVersion := strings.TrimSpace(version)
		if date != "" {
			fullVersion += fmt.Sprintf(" (%s, %s)", date, commit)
		}
		fmt.Printf("qbench %s", fullVersion)
		fmt.Println()
		os.Exit(0)
	}

	if *dbname == "" {
		log.Fatal("Database name required (-d)")
	}

	if *totalRequests <= 0 {
		log.Fatal("Total requests must be greater than 0")
	}

	if *warmup < 0 {
		log.Fatal("Warmup queries cannot be negative")
	}

	if *concurrency <= 0 {
		log.Fatal("Concurrency must be greater than 0")
	}

	driver := "mysql"
	if _, err := os.Stat(*dbname); err == nil {
		driver = "sqlite3"
	}

	var dsn string
	switch driver {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", *user, *password, *host, *port, *dbname)
	case "sqlite3":
		dsn = *dbname
	}

	return execOptions{
		driver:        driver,
		dsn:           dsn,
		host:          *host,
		port:          *port,
		user:          *user,
		dbname:        *dbname,
		query:         *query,
		totalRequests: *totalRequests,
		concurrency:   *concurrency,
		warmup:        *warmup,
	}
}

func openDB(options execOptions) *sql.DB {
	db, err := sql.Open(options.driver, options.dsn)
	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(options.concurrency)
	db.SetMaxIdleConns(options.concurrency)
	db.SetConnMaxLifetime(time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	return db
}

func printConnectionInfo(options execOptions) {
	switch options.driver {
	case "mysql":
		fmt.Printf("Connection: %s@%s:%d/%s\n", options.user, options.host, options.port, options.dbname)
	case "sqlite3":
		fmt.Printf("Connection: sqlite3://%s\n", options.dbname)
	}
	fmt.Printf("Query: %s\n", options.query)
	fmt.Println()
}

func runWarmup(db *sql.DB, query string, count int, concurrency int) {
	if count <= 0 {
		return
	}

	fmt.Printf("Warming up with %d queries and concurrency %d...\n", count, concurrency)

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rows, err := db.Query(query)
			if err == nil {
				for rows.Next() {
				}
				rows.Close()
			}
		}()
	}

	wg.Wait()
	fmt.Println("Warmup complete.")
	fmt.Println()
}

func runBenchmark(db *sql.DB, options execOptions) benchmarkResults {
	fmt.Printf("Running %d queries with concurrency %d...\n", options.totalRequests, options.concurrency)

	var wg sync.WaitGroup
	var errors int64
	var fetchedRows int64
	var fetchedBytes int64
	latencyChan := make(chan float64, options.totalRequests)

	startTime := time.Now()

	sem := make(chan struct{}, options.concurrency)

	for i := 0; i < options.totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()

			rows, err := db.Query(options.query)
			if err != nil {
				atomic.AddInt64(&errors, 1)
			} else {
				cols, _ := rows.Columns()
				for rows.Next() {
					atomic.AddInt64(&fetchedRows, 1)
					values := make([]sql.RawBytes, len(cols))
					scanArgs := make([]interface{}, len(cols))
					for i := range values {
						scanArgs[i] = &values[i]
					}
					if err := rows.Scan(scanArgs...); err == nil {
						for _, v := range values {
							atomic.AddInt64(&fetchedBytes, int64(len(v)))
						}
					}
				}
				rows.Close()
			}

			elapsed := time.Since(start).Seconds() * 1000
			latencyChan <- elapsed
		}()
	}

	wg.Wait()
	close(latencyChan)

	totalTime := time.Since(startTime).Seconds()

	latencies := make([]float64, 0, options.totalRequests)
	for l := range latencyChan {
		latencies = append(latencies, l)
	}
	sort.Float64s(latencies)

	return benchmarkResults{
		totalTime:     totalTime,
		completeReqs:  options.totalRequests - int(errors),
		failedReqs:    errors,
		rps:           float64(options.totalRequests) / totalTime,
		fetchedRows:   fetchedRows,
		rowsPerQuery:  float64(fetchedRows) / float64(options.totalRequests),
		fetchedBytes:  fetchedBytes,
		bytesPerQuery: float64(fetchedBytes) / float64(options.totalRequests),
		avgLatency:    average(latencies),
		p95Latency:    percentile(latencies, 95),
		p99Latency:    percentile(latencies, 99),
	}
}

func printResults(results benchmarkResults) {
	fmt.Println("\n---- Benchmark Results ----")
	fmt.Printf("Total time:        %.3f sec\n", results.totalTime)
	fmt.Printf("Complete requests: %d\n", results.completeReqs)
	fmt.Printf("Failed requests:   %d\n", results.failedReqs)
	fmt.Printf("Requests/sec:      %.2f\n", results.rps)
	fmt.Printf("Fetched rows:      %d\n", results.fetchedRows)
	fmt.Printf("Rows/query:        %.0f\n", results.rowsPerQuery)
	fmt.Printf("Fetched data:      %d bytes\n", results.fetchedBytes)
	fmt.Printf("Bytes/query:       %.0f\n", results.bytesPerQuery)
	fmt.Printf("Average latency:   %.2f ms\n", results.avgLatency)
	fmt.Printf("P95 latency:       %.2f ms\n", results.p95Latency)
	fmt.Printf("P99 latency:       %.2f ms\n", results.p99Latency)
}

func average(nums []float64) float64 {
	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	return sum / float64(len(nums))
}

func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(p)/100*float64(len(sorted)))) - 1
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func main() {
	options := parseFlags()
	db := openDB(options)
	defer db.Close()

	printConnectionInfo(options)
	runWarmup(db, options.query, options.warmup, options.concurrency)
	results := runBenchmark(db, options)
	printResults(results)
}
