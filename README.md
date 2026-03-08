# qbench

[![build](https://github.com/sibprogrammer/qbench/workflows/build/badge.svg)](https://github.com/sibprogrammer/qbench/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/sibprogrammer/qbench)](https://goreportcard.com/report/github.com/sibprogrammer/qbench)
[![Codecov](https://codecov.io/gh/sibprogrammer/qbench/branch/master/graph/badge.svg?token=ZFMT4JSKC3)](https://codecov.io/gh/sibprogrammer/qbench)
[![Scc Count](https://sloc.xyz/github/sibprogrammer/qbench/)](https://github.com/sibprogrammer/qbench/)

A small portable utility to benchmark the performance of SQL queries. Like `ab` (ApacheBench) for SQL world.

# Supported Databases

Currently supported databases are:
- MySQL/MariaDB
- SQLite

# Usage

Perform 1000 iterations of the query and print the results:

```
qbench -n 1000 -d testdb -e "SELECT * FROM users"
```

Output may look like this:

```
Connection: root@127.0.0.1:3306/testdb
Query: SELECT * FROM users

Running 1000 queries with concurrency 1...

---- Benchmark Results ----
Total time:        1.55 sec
Complete requests: 1000
Failed requests:   0
Requests/sec:      644.60
Fetched rows:      301000
Rows/query:        301
Fetched data:      18435000 bytes
Bytes/query:       18435
Average latency:   1.53 ms
P95 latency:       1.93 ms
P99 latency:       2.33 ms
```

# Installation

System-wide installation using the installer script:
```
curl -sSL https://bit.ly/install-qbench | sudo bash
```

Install to the INSTALL_DIR directory, without sudo:
```
curl -sSL https://bit.ly/install-qbench | INSTALL_DIR=$(pwd) bash
```

If you have Go toolchain installed, you can use the following command to install the utility:

```
go install github.com/sibprogrammer/qbench@latest
```
