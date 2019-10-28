package server

import (
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

	expectedSeq   uint32
	sendQuotaTick int

	queue *RPacketQueue
}

func newRequest(o *Account, idx uint16) *Request {
	r := &Request{owner: o, idx: idx}
	r.queue = newRPacketQueue()

	return r
}

func (r *Request) dofree() {
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}

	r.tunnel = nil
	r.sreq = nil
}

func (r *Request) onClientFinished() {
	if r.conn != nil {
		r.conn.CloseWrite()
	}
}

func (r *Request) onClientData(seq uint32, data []byte) {
	if r.conn != nil {
		// queue to heap
		r.queue.append(seq, data)

		// loop heap
		r.doSend()
	}
}

func (r *Request) proxy() {
	// log.Println("proxy ...")
	c := r.conn
	if c == nil {
		log.Println("proxy failed, conn is nil")
		return
	}

	defer c.Close()

	r.tunnel.sendRequestCreate(r)

	if !r.isUsed {
		log.Println("proxy failed, req is not used")
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)

		if !r.isUsed {
			// request is free!
			log.Println("proxy read, request is free, discard data:", n)
			break
		}

		t := r.tunnel
		if t == nil {
			log.Println("proxy read, no tunnel valid, discard data:", n)
			break
		}

		if err != nil {
			log.Println("proxy read failed:", err)
			t.onRequestTerminate(r)
			break
		}

		if n == 0 {
			log.Println("proxy read, server half close")
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
			if err != nil {
				log.Println("request sendto failed:", err)

				break
			}

			// move to next seq
			r.expectedSeq++
			r.sendQuotaTick++

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
