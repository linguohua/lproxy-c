package server

import (
	"io"
	"lproxyc/socks5"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	defaultQuotaReport = 20
)

// Request request
type Request struct {
	isUsed bool
	idx    uint16
	tag    uint16
	owner  *Account
	tunnel *Tunnel
	sreq   *socks5.SocksRequest

	writeLock sync.Mutex
	inSending bool
	conn      *net.TCPConn

	expectedSeq       uint32
	sendQuotaTick     int
	lastSeqNo         uint32
	pendingClosed     bool
	pendingHalfClosed bool

	queue *RPacketQueue
}

func newRequest(o *Account, idx uint16) *Request {
	r := &Request{owner: o, idx: idx}
	r.queue = newRPacketQueue()

	return r
}

func (r *Request) use(sreq *socks5.SocksRequest, t *Tunnel) {
	r.pendingClosed = false
	r.pendingHalfClosed = false
	r.lastSeqNo = 0

	r.sreq = sreq
	r.conn = sreq.Conn.(*net.TCPConn)

	r.tunnel = t
	r.expectedSeq = 0

	r.tag++
	r.isUsed = true
}

func (r *Request) unuse() {
	r.tunnel = nil
	r.sreq = nil
	r.tag++
	r.isUsed = false
	r.queue.clear()

	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

func (r *Request) onServerFinished(lastSeqNo uint32) {
	r.lastSeqNo = lastSeqNo
	if r.expectedSeq < lastSeqNo {
		// we has more data to recv
		r.pendingHalfClosed = true
		log.Printf("req %d:%d onServerFinished pending, last:%d",
			r.idx, r.tag, lastSeqNo)
	} else {
		if r.conn != nil {
			r.conn.CloseWrite()
		}
	}
}

func (r *Request) onServerClosed(lastSeqNo uint32) bool {
	r.lastSeqNo = lastSeqNo
	if r.expectedSeq < lastSeqNo {
		// we has more data to recv
		r.pendingClosed = true
		log.Printf("req %d:%d onServerClosed pending, last:%d",
			r.idx, r.tag, lastSeqNo)
		return false
	}

	return true
}

func (r *Request) onClientData(seq uint32, data []byte) {
	if r.conn != nil {
		// queue to heap
		r.queue.append(seq, data)

		// loop heap
		r.doSend()

		if r.pendingHalfClosed {
			log.Printf("req %d:%d has pendingHalfClosed, expected:%d, last:%d",
				r.idx, r.tag, r.expectedSeq, r.lastSeqNo)
			if r.expectedSeq >= r.lastSeqNo && r.conn != nil {
				r.conn.CloseWrite()
			}
		}

		if r.pendingClosed {
			log.Printf("req %d:%d has pendingClosed, expected:%d, last:%d",
				r.idx, r.tag, r.expectedSeq, r.lastSeqNo)
			if r.expectedSeq >= r.lastSeqNo && r.tunnel != nil {
				r.tunnel.freeRequest(r.idx, r.tag)
			}
		}
	}
}

func (r *Request) proxy() {
	// log.Println("proxy ...")
	c := r.conn
	if c == nil {
		log.Printf("request %d:%d failed, conn is nil",
			r.idx, r.tag)
		return
	}

	defer c.Close()

	r.tunnel.sendRequestCreate(r)

	if !r.isUsed {
		log.Printf("request %d:%d failed, req is not used",
			r.idx, r.tag)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)

		if !r.isUsed {
			// request is free!
			log.Printf("request %d:%d read, request is free, discard data:%d",
				r.idx, r.tag, n)
			break
		}

		t := r.tunnel
		if t == nil {
			log.Printf("request %d:%d read read, no tunnel valid, discard data:%d",
				r.idx, r.tag, n)
			break
		}

		if err != nil {
			if err == io.EOF {
				log.Printf("request %d:%d read, client half close", r.idx, r.tag)
				t.onRequestHalfClosed(r)
			} else {
				log.Printf("request %d:%d  read failed:%v", r.idx, r.tag, err)
				t.onRequestTerminate(r)
			}

			break
		}

		if n == 0 {
			log.Printf("request %d:%d read, server half close", r.idx, r.tag)
			t.onRequestHalfClosed(r)
			break
		}

		t.onRequestData(r, buf[:n])
	}
}

func writeAll(buf []byte, nc net.Conn) error {
	wrote := 0
	l := len(buf)
	for {
		nc.SetWriteDeadline(time.Now().Add(time.Second))
		n, err := nc.Write(buf[wrote:])
		if err != nil {
			// discard connection
			nc.Close()

			return err
		}

		wrote = wrote + n
		if wrote == l {
			break
		}
	}

	return nil
}

func (r *Request) doSend() {
	if r.inSending {
		return
	}

	// don't return without reset r.inSending
	r.inSending = true

	for {
		if r.queue.size() < 1 {
			// no remain packet need to send
			break
		}

		if !r.isUsed {
			break
		}

		header := r.queue.head()
		// log.Printf("doSend, header seq:%d, expect:%d", header.seqNo, r.expectedSeq)
		// only send expected
		if header.seqNo == r.expectedSeq {
			header = r.queue.pop()
			err := r.sendto(header.data)
			// move to next seq
			r.expectedSeq++
			r.sendQuotaTick++

			if err != nil {
				log.Printf("request %d:%d sendto failed. force close:%v",
					r.idx, r.tag, err)

				// force free
				if r.tunnel != nil {
					r.tunnel.onRequestTerminate(r)
				}

				break
			}

			if r.sendQuotaTick == defaultQuotaReport {
				r.sendQuotaTick = 0
				r.tunnel.onQuotaReport(r, defaultQuotaReport)
			}
		} else {
			break
		}
	}

	r.inSending = false
}

func (r *Request) sendto(buf []byte) error {
	r.writeLock.Lock()
	defer r.writeLock.Unlock()

	return writeAll(buf, r.conn)
}
