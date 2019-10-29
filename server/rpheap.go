package server

import (
	"container/heap"
)

// RPacket request packet
type RPacket struct {
	seqNo uint32
	data  []byte
}

// An IntHeap is a min-heap of ints.
type IntHeap []*RPacket

func (h IntHeap) Len() int           { return len(h) }
func (h IntHeap) Less(i, j int) bool { return h[i].seqNo < h[j].seqNo }
func (h IntHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

// Push implement heap interface
func (h *IntHeap) Push(x interface{}) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	*h = append(*h, x.(*RPacket))
}

// Pop implement heap interface
func (h *IntHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// RPacketQueue queue
type RPacketQueue struct {
	h IntHeap
}

func newRPacketQueue() *RPacketQueue {
	q := &RPacketQueue{}
	// 16 capacity
	q.h = make([]*RPacket, 0, 16)
	return q
}

func (q *RPacketQueue) clear() {
	q.h = make([]*RPacket, 0, 16)
}

func (q *RPacketQueue) size() int {
	return q.h.Len()
}

func (q *RPacketQueue) append(seq uint32, data []byte) {
	rp := &RPacket{
		seqNo: seq,
		data:  data,
	}

	heap.Push(&q.h, rp)

	//log.Printf("RPacketQueue append, seq:%d, total len:%d", seq, q.size())
}

func (q *RPacketQueue) pop() *RPacket {
	if q.size() < 1 {
		return nil
	}

	p := heap.Pop(&q.h).(*RPacket)
	//log.Printf("RPacketQueue pop, seq:%d, len:%d", p.seqNo, q.size())

	return p
}

func (q *RPacketQueue) head() *RPacket {
	n := q.size()
	if n < 1 {
		return nil
	}

	return q.h[0]
}
