package main

import (
	"context"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	queueCapacity = 10000
	maxBatchRows  = 200
	flushInterval = 5 * time.Second
)

type tableRow struct {
	table string
	row   any
}

type rowQueue struct {
	target   atomic.Pointer[clickhouseTarget]
	rows     chan tableRow
	shutdown chan chan struct{}
}

func newRowQueue() *rowQueue {
	q := &rowQueue{
		rows:     make(chan tableRow, queueCapacity),
		shutdown: make(chan chan struct{}),
	}
	go q.run()
	return q
}

func (q *rowQueue) enqueue(table string, row any) {
	select {
	case q.rows <- tableRow{table: table, row: row}:
	default:
		log.Warnf("clickhouse-log: queue full, dropped %s row", table)
	}
}

func (q *rowQueue) close() {
	done := make(chan struct{})
	q.shutdown <- done
	<-done
}

func (q *rowQueue) run() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	var schemaReadyFor clickhouseTarget
	batch := map[string][]any{}
	pending := 0
	flush := func() {
		if pending == 0 {
			return
		}
		schemaReadyFor = q.flush(batch, schemaReadyFor)
		batch = map[string][]any{}
		pending = 0
	}
	for {
		select {
		case r := <-q.rows:
			batch[r.table] = append(batch[r.table], r.row)
			pending++
			if pending >= maxBatchRows {
				flush()
			}
		case <-ticker.C:
			flush()
		case done := <-q.shutdown:
			for drained := false; !drained; {
				select {
				case r := <-q.rows:
					batch[r.table] = append(batch[r.table], r.row)
					pending++
				default:
					drained = true
				}
			}
			flush()
			close(done)
			return
		}
	}
}

func (q *rowQueue) flush(batch map[string][]any, schemaReadyFor clickhouseTarget) clickhouseTarget {
	target := q.target.Load()
	if target == nil || target.URL == "" {
		return schemaReadyFor
	}
	ctx := context.Background()
	if *target != schemaReadyFor {
		if errSchema := target.ensureSchema(ctx); errSchema != nil {
			log.Warnf("clickhouse-log: create schema failed, dropped batch: %v", errSchema)
			return schemaReadyFor
		}
		schemaReadyFor = *target
	}
	for table, rows := range batch {
		if errInsert := target.insert(ctx, table, rows); errInsert != nil {
			log.Warnf("clickhouse-log: insert %d %s rows failed: %v", len(rows), table, errInsert)
		}
	}
	return schemaReadyFor
}
