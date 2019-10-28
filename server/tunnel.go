package server

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	cMDNone              = 0
	cMDReqData           = 1
	cMDReqCreated        = 2
	cMDReqClientClosed   = 3
	cMDReqClientFinished = 4
	cMDReqServerFinished = 5
	cMDReqServerClosed   = 6
	cMDReqClientQuota    = 7
)

// Tunnel tunnel
type Tunnel struct {
	id   int
	conn *websocket.Conn

	writeLock sync.Mutex
	waitping  int

	owner  *Account
	reqMap map[uint16]*Request
}

func newTunnel(id int, conn *websocket.Conn, o *Account) *Tunnel {

	t := &Tunnel{
		id:     id,
		conn:   conn,
		owner:  o,
		reqMap: make(map[uint16]*Request),
	}

	conn.SetPingHandler(func(data string) error {
		t.writePong([]byte(data))
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong([]byte(data))
		return nil
	})

	return t
}

func (t *Tunnel) serve() {
	// loop read websocket message
	c := t.conn
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("Tunnel read failed:", err)
			break
		}

		// log.Println("Tunnel recv message, len:", len(message))
		err = t.onTunnelMessage(message)
		if err != nil {
			log.Println("Tunnel onTunnelMessage failed:", err)
			break
		}
	}

	t.onClose()
}

func (t *Tunnel) keepalive() {
	if t.waitping > 3 {
		t.conn.Close()
		return
	}

	if t.conn == nil {
		return
	}

	t.writeLock.Lock()
	now := time.Now().Unix()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(now))
	t.conn.WriteMessage(websocket.PingMessage, b)
	t.writeLock.Unlock()

	t.waitping++
}

func (t *Tunnel) writePong(msg []byte) {
	if t.conn == nil {
		return
	}

	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.PongMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) write(msg []byte) {
	if t.conn == nil {
		return
	}

	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.BinaryMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) onPong(msg []byte) {
	t.waitping = 0
}

func (t *Tunnel) onClose() {
	for _, r := range t.reqMap {
		t.handleRequestClosed(r.idx, r.tag)
	}

	t.reqMap = make(map[uint16]*Request)
}

func (t *Tunnel) remember(r *Request) {
	t.reqMap[r.idx] = r
}

func (t *Tunnel) forget(r *Request) {
	delete(t.reqMap, r.idx)
}

func (t *Tunnel) onTunnelMessage(message []byte) error {
	if len(message) < 5 {
		return fmt.Errorf("invalid tunnel message")
	}

	cmd := message[0]
	idx := binary.LittleEndian.Uint16(message[1:])
	tag := binary.LittleEndian.Uint16(message[3:])

	switch cmd {
	case cMDReqData:
		t.handleRequestData(idx, tag, message[5:])
	case cMDReqServerFinished:
		t.handleRequestFinished(idx, tag)
	case cMDReqServerClosed:
		t.handleRequestClosed(idx, tag)
	default:
		log.Printf("onTunnelMessage, unsupport tunnel cmd:%d", cmd)
	}

	return nil
}

func (t *Tunnel) handleRequestData(idx uint16, tag uint16, message []byte) {
	req, err := t.owner.reqq.get(idx, tag)
	if err != nil {
		log.Println("handleRequestData, get req failed:", err)
		return
	}

	seqno := binary.LittleEndian.Uint32(message)
	req.onClientData(seqno, message[4:])
}

func (t *Tunnel) handleRequestFinished(idx uint16, tag uint16) {
	req, err := t.owner.reqq.get(idx, tag)
	if err != nil {
		//log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientFinished()
}

func (t *Tunnel) handleRequestClosed(idx uint16, tag uint16) {
	err := t.owner.reqq.free(idx, tag)
	if err != nil {
		//log.Println("handleRequestClosed, get req failed:", err)
		return
	}
}

func (t *Tunnel) onRequestTerminate(req *Request) {
	// send close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqClientClosed
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)

	t.handleRequestClosed(req.idx, req.tag)
}

func (t *Tunnel) onRequestHalfClosed(req *Request) {
	// send half-close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqClientFinished
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)
}

func (t *Tunnel) onQuotaReport(req *Request, quota uint16) {
	buf := make([]byte, 5+2)
	buf[0] = cMDReqClientQuota
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)
	binary.LittleEndian.PutUint16(buf[5:], quota)

	t.write(buf)
}

func (t *Tunnel) onRequestData(req *Request, data []byte) {
	buf := make([]byte, 5+len(data))
	buf[0] = cMDReqData
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)
	copy(buf[5:], data)

	t.write(buf)
}

func (t *Tunnel) sendRequestCreate(req *Request) {
	var addressLength int
	address := req.sreq.DestAddr
	var addressBytes []byte
	if address.FQDN != "" {
		addressLength = len(address.FQDN)
		addressBytes = []byte(address.FQDN)
	} else {
		addressLength = len(address.IP)
		addressBytes = address.IP
	}

	// log.Printf("sendRequestCreate, addressLength:%d", addressLength)

	buf := make([]byte, 5+1+1+addressLength+2) // addressType + address + port
	buf[0] = cMDReqCreated
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)
	buf[5] = 1                   // address type always is 1
	buf[6] = byte(addressLength) // address type always is 1

	copy(buf[7:], addressBytes)
	binary.LittleEndian.PutUint16(buf[7+addressLength:], uint16(address.Port))

	t.write(buf)
}
