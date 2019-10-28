package server

import (
	"fmt"
	"log"
	"lproxyc/socks5"
	"net"
)

// Reqq request queue
type Reqq struct {
	owner *Account
	array []*Request

	freeSlots []uint16
	freeHead  int
	freeTail  int
	freeCount int
}

func newReqq(cap int, o *Account) *Reqq {
	reqq := &Reqq{owner: o}

	reqq.array = make([]*Request, cap)
	reqq.freeSlots = make([]uint16, cap)
	reqq.freeTail = cap
	reqq.freeHead = 0
	reqq.freeCount = cap

	for i := 0; i < cap; i++ {
		reqq.array[i] = newRequest(o, uint16(i))
		reqq.freeSlots[i] = uint16(i)
	}

	return reqq
}

func (q *Reqq) isEmpty() bool {
	return q.freeCount == 0
}

func (q *Reqq) isFulled() bool {
	return q.freeCount == len(q.freeSlots)
}

func (q *Reqq) pop() uint16 {
	if q.isEmpty() {
		log.Panic("Reqq is empty, pop failed")
	}

	if q.freeHead >= len(q.freeSlots) {
		q.freeHead = 0
	}

	idx := q.freeSlots[q.freeHead]
	q.freeHead++
	q.freeCount--
	// log.Println("pop:", idx)
	return idx
}

func (q *Reqq) push(v uint16) {
	// log.Println("push:", v)
	if q.isFulled() {
		log.Panic("Reqq is fulled, push failed")
	}

	if q.freeTail >= len(q.freeSlots) {
		q.freeTail = 0
	}

	q.freeSlots[q.freeTail] = v
	q.freeTail++
	q.freeCount++
}

func (q *Reqq) alloc(sreq *socks5.SocksRequest, t *Tunnel) (*Request, error) {
	if q.isEmpty() {
		return nil, fmt.Errorf("queue is empty")
	}

	idx := q.pop()
	req := q.array[idx]
	if req.isUsed {
		return nil, fmt.Errorf("slots idx point to in-used req")
	}

	req.tag++
	req.isUsed = true
	req.sreq = sreq
	req.conn = sreq.Conn.(*net.TCPConn)

	req.tunnel = t
	req.expectedSeq = 0

	return req, nil
}

func (q *Reqq) free(idx uint16, tag uint16) error {
	if idx >= uint16(len(q.array)) {
		return fmt.Errorf("free, idx %d >= len %d", idx, uint16(len(q.array)))
	}

	req := q.array[idx]
	if !req.isUsed {
		return fmt.Errorf("free, req %d:%d is in not used", idx, tag)
	}

	if req.tag != tag {
		return fmt.Errorf("free, req %d:%d is in not match tag %d", idx, tag, req.tag)
	}

	req.tag++
	req.isUsed = false
	q.push(idx)

	req.dofree()

	log.Printf("reqq free req %d:%d", idx, tag)

	return nil
}

func (q *Reqq) get(idx uint16, tag uint16) (*Request, error) {
	if idx >= uint16(len(q.array)) {
		return nil, fmt.Errorf("get, idx %d >= len %d", idx, uint16(len(q.array)))
	}

	req := q.array[idx]
	if !req.isUsed {
		return nil, fmt.Errorf("get, req %d:%d is not in used", idx, tag)
	}

	if req.tag != tag {
		return nil, fmt.Errorf("get, req %d:%d tag not match %d", idx, req.tag, tag)
	}

	return req, nil
}

func (q *Reqq) cleanup() {
	for _, r := range q.array {
		if r.isUsed {
			q.free(r.idx, r.tag)
		}
	}
}
