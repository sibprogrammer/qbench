package main

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/pflag"
)

func main() {
	host := pflag.StringP("host", "h", "127.0.0.1", "MySQL host")
	port := pflag.IntP("port", "P", 3306, "MySQL port")
	user := pflag.StringP("user", "u", "root", "MySQL user")
	pass := pflag.StringP("password", "p", "", "MySQL password")
	dbname := pflag.StringP("database", "d", "", "Database name (or file path for SQLite)")
	query := pflag.StringP("execute", "e", "SELECT 1", "SQL query")
	totalRequests := pflag.IntP("requests", "n", 1000, "Total number of requests")
	concurrency := pflag.IntP("concurrency", "c", 1, "Concurrency level")
	warmup := pflag.IntP("warmup", "w", 0, "Number of warmup queries before benchmarking")
	pflag.Parse()

	if *dbname == "" {
		log.Fatal("Database name required (-d)")
	}

	if *totalRequests <= 0 {
		log.Fatal("Total requests must be greater than 0")
	}

	driver := "mysql"
	if _, err := os.Stat(*dbname); err == nil {
		driver = "sqlite3"
	}

	var dsn string
	switch driver {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", *user, *pass, *host, *port, *dbname)
	case "sqlite3":
		dsn = *dbname
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(*concurrency)
	db.SetMaxIdleConns(*concurrency)
	db.SetConnMaxLifetime(time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	switch driver {
	case "mysql":
		fmt.Printf("Connection: %s@%s:%d/%s\n", *user, *host, *port, *dbname)
	case "sqlite3":
		fmt.Printf("Connection: sqlite3://%s\n", *dbname)
	}
	fmt.Printf("Query: %s\n", *query)
	fmt.Println()

	runWarmup(db, *query, *warmup, *concurrency)

	fmt.Printf("Running %d queries with concurrency %d...\n", *totalRequests, *concurrency)

	var wg sync.WaitGroup
	var errors int64
	var fetchedRows int64
	var fetchedBytes int64
	latencies := make([]float64, 0, *totalRequests)
	latencyChan := make(chan float64, *totalRequests)

	startTime := time.Now()

	sem := make(chan struct{}, *concurrency)

	for i := 0; i < *totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()

			rows, err := db.Query(*query)
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

	for l := range latencyChan {
		latencies = append(latencies, l)
	}

	sort.Float64s(latencies)

	completeRequests := *totalRequests - int(errors)
	avg := average(latencies)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	rps := float64(*totalRequests) / totalTime
	rowsPerQuery := float64(fetchedRows) / float64(*totalRequests)
	bytesPerQuery := float64(fetchedBytes) / float64(*totalRequests)

	fmt.Println("\n---- Benchmark Results ----")
	fmt.Printf("Total time:        %.2f sec\n", totalTime)
	fmt.Printf("Complete requests: %d\n", completeRequests)
	fmt.Printf("Failed requests:   %d\n", errors)
	fmt.Printf("Requests/sec:      %.2f\n", rps)
	fmt.Printf("Fetched rows:      %d\n", fetchedRows)
	fmt.Printf("Rows/query:        %.0f\n", rowsPerQuery)
	fmt.Printf("Fetched data:      %d bytes\n", fetchedBytes)
	fmt.Printf("Bytes/query:       %.0f\n", bytesPerQuery)
	fmt.Printf("Average latency:   %.2f ms\n", avg)
	fmt.Printf("P95 latency:       %.2f ms\n", p95)
	fmt.Printf("P99 latency:       %.2f ms\n", p99)
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
