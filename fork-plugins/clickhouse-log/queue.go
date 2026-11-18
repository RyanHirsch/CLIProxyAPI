package main

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	queueCapacity   = 500
	maxBatchRows    = 200
	maxRetainedRows = 500
	flushInterval   = 5 * time.Second
)

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
	pending := newBatch()
	flush := func() {
		if pending.rows == 0 {
			return
		}
		schemaReadyFor = q.flush(pending, schemaReadyFor)
		if pending.rows > maxRetainedRows {
			log.Warnf("clickhouse-log: still unreachable, dropped %d retained rows", pending.rows)
			pending.clear()
		}
	}
	for {
		select {
		case r := <-q.rows:
			pending.add(r)
			if pending.rows >= maxBatchRows && !pending.retrying {
				flush()
			}
		case <-ticker.C:
			flush()
		case done := <-q.shutdown:
			for drained := false; !drained; {
				select {
				case r := <-q.rows:
					pending.add(r)
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

func (q *rowQueue) flush(pending *batch, schemaReadyFor clickhouseTarget) clickhouseTarget {
	target := q.target.Load()
	if target == nil || target.URL == "" {
		pending.clear()
		return schemaReadyFor
	}
	ctx := context.Background()
	if *target != schemaReadyFor {
		if errSchema := target.ensureSchema(ctx); errSchema != nil {
			if errors.Is(errSchema, errUnreachable) {
				pending.retrying = true
				log.Warnf("clickhouse-log: create schema failed, will retry %d rows: %v", pending.rows, errSchema)
				return schemaReadyFor
			}
			log.Warnf("clickhouse-log: create schema failed, dropped %d rows: %v", pending.rows, errSchema)
			pending.clear()
			return schemaReadyFor
		}
		schemaReadyFor = *target
	}
	pending.retrying = false
	for table, rows := range pending.tables {
		errInsert := target.insert(ctx, table, rows)
		switch {
		case errInsert == nil:
			pending.remove(table)
		case errors.Is(errInsert, errUnreachable):
			pending.retrying = true
			log.Warnf("clickhouse-log: insert %d %s rows failed, will retry: %v", len(rows), table, errInsert)
		default:
			pending.remove(table)
			log.Warnf("clickhouse-log: insert %d %s rows failed, dropped: %v", len(rows), table, errInsert)
		}
	}
	return schemaReadyFor
}
