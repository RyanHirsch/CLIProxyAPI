package main

type tableRow struct {
	table string
	row   any
}

type batch struct {
	tables   map[string][]any
	rows     int
	retrying bool
}

func newBatch() *batch {
	return &batch{tables: map[string][]any{}}
}

func (b *batch) add(r tableRow) {
	b.tables[r.table] = append(b.tables[r.table], r.row)
	b.rows++
}

func (b *batch) remove(table string) {
	b.rows -= len(b.tables[table])
	delete(b.tables, table)
}

func (b *batch) clear() {
	b.tables = map[string][]any{}
	b.rows = 0
	b.retrying = false
}
