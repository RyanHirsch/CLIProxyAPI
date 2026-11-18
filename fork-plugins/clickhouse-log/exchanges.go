package main

import "sync"

type exchange struct {
	requestBody  []byte
	responseBody []byte
}

type exchangeLog struct {
	mu        sync.Mutex
	exchanges map[string]*exchange
}

func newExchangeLog() *exchangeLog {
	return &exchangeLog{exchanges: map[string]*exchange{}}
}

func (l *exchangeLog) recordRequest(requestID string, body []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entry(requestID)
	if entry.requestBody == nil {
		entry.requestBody = body
	}
}

func (l *exchangeLog) appendResponse(requestID string, body []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entry(requestID)
	entry.responseBody = append(entry.responseBody, body...)
}

func (l *exchangeLog) finish(requestID string) exchange {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.exchanges[requestID]
	if !ok {
		return exchange{}
	}
	delete(l.exchanges, requestID)
	return *entry
}

func (l *exchangeLog) entry(requestID string) *exchange {
	entry, ok := l.exchanges[requestID]
	if !ok {
		entry = &exchange{}
		l.exchanges[requestID] = entry
	}
	return entry
}
