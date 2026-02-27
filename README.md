# qbench

A small portable utility to benchmark the performance of SQL queries. Like `ab` (ApacheBench) for SQL world.

# Usage

Perform 1000 iterations of the query and print the results:

```
qbench -c 1000 -d testdb -e "SELECT * FROM users"
```

Output may look like this:

```
Connection: admin@127.0.0.1:3306/testdb
Query: SELECT * FROM users

Running 1000 queries with concurrency 1...

---- Benchmark Results ----
Total time:        1.21 sec
Complete requests: 1000
Failed requests:   0
Requests/sec:      826.23
Average latency:   1.19 ms
P95 latency:       1.66 ms
P99 latency:       3.44 ms
```
