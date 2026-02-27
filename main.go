package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	host := flag.String("host", "127.0.0.1", "MySQL host")
	port := flag.Int("port", 3306, "MySQL port")
	user := flag.String("user", "root", "MySQL user")
	pass := flag.String("pass", "", "MySQL password")
	dbname := flag.String("db", "", "Database name")
	query := flag.String("q", "SELECT 1", "SQL query")
	totalRequests := flag.Int("n", 1000, "Total number of requests")
	concurrency := flag.Int("c", 1, "Concurrency level")
	flag.Parse()

	if *dbname == "" {
		log.Fatal("Database name required (-db)")
	}

	if *totalRequests <= 0 {
		log.Fatal("Total requests must be greater than 0")
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		*user, *pass, *host, *port, *dbname)

	db, err := sql.Open("mysql", dsn)
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

	fmt.Printf("Connection: %s@%s:%d/%s\n", *user, *host, *port, *dbname)
	fmt.Printf("Query: %s\n", *query)
	fmt.Println()
	fmt.Printf("Running %d queries with concurrency %d...\n", *totalRequests, *concurrency)

	var wg sync.WaitGroup
	var errors int64
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
				for rows.Next() {
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

	avg := average(latencies)
	p95 := percentile(latencies, 95)
	p99 := percentile(latencies, 99)
	rps := float64(*totalRequests) / totalTime

	fmt.Println("\n---- Benchmark Results ----")
	fmt.Printf("Total time:        %.2f sec\n", totalTime)
	fmt.Printf("Complete requests: %d\n", *totalRequests-int(errors))
	fmt.Printf("Failed requests:   %d\n", errors)
	fmt.Printf("Requests/sec:      %.2f\n", rps)
	fmt.Printf("Average latency:   %.2f ms\n", avg)
	fmt.Printf("P95 latency:       %.2f ms\n", p95)
	fmt.Printf("P99 latency:       %.2f ms\n", p99)
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
