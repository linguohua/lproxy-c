package server

import (
	"fmt"
	"lproxyc/socks5"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// Account account
type Account struct {
	uuid    string
	url     string
	tunnels []*Tunnel

	reqq *Reqq

	nextTunnelIdx int
}

func newAccount(reqCap int, uuid string, url string, tunnelCount int) *Account {
	a := &Account{
		uuid:    uuid,
		url:     url,
		tunnels: make([]*Tunnel, tunnelCount),
	}

	reqq := newReqq(reqCap, a)
	a.reqq = reqq

	return a
}

func (a *Account) keepalive() {
	for _, t := range a.tunnels {
		t.keepalive()
	}
}

func (a *Account) getTunnel() *Tunnel {
	idx := a.nextTunnelIdx
	for i := idx; i < len(a.tunnels); i++ {
		t := a.tunnels[i]
		if t == nil || t.conn == nil {
			continue
		}

		a.nextTunnelIdx = (i + 1) % len(a.tunnels)
		return t
	}

	for i := 0; i < idx; i++ {
		t := a.tunnels[i]
		if t == nil || t.conn == nil {
			continue
		}

		a.nextTunnelIdx = (i + 1) % len(a.tunnels)
		return t
	}

	return nil
}

// HandleRequest proc socks5 request
func (a *Account) HandleRequest(req *socks5.SocksRequest) error {
	// alloc request
	t := a.getTunnel()
	if t == nil {
		log.Println("HandleRequest failed, getTunnel nil")
		err := fmt.Errorf("no tunnel")
		return err
	}

	r, err := a.reqq.alloc(req, t)

	if err != nil {
		log.Println("HandleRequest failed, req alloc failed:", err)

		return err
	}

	r.proxy()

	return nil
}

func (a *Account) buildTunnels() {
	for i := 0; i < len(a.tunnels); i++ {
		go tunnelRunner(a, i)
	}

	go tunnelKeepalive(a)
}

func tunnelRunner(a *Account, idx int) {
	url := fmt.Sprintf("%s?uuid=%s", a.url, a.uuid)

	for {
		log.Println("websocket dail to:", url)
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			log.Printf("websocket dial failed:%v, re-build later", err)
			time.Sleep(10 * time.Second)
			continue
		}

		tunnel := newTunnel(idx, c, a)
		a.tunnels[idx] = tunnel

		{
			defer func() {
				c.Close()
				a.tunnels[idx] = nil
			}()

			tunnel.serve()
		}

		log.Println("tunnel break, re-build later")
		time.Sleep(15 * time.Second)
	}
}

func tunnelKeepalive(a *Account) {
	for {
		time.Sleep(time.Second * 30)

		for _, t := range a.tunnels {
			if t != nil {
				t.keepalive()
			}
		}
	}
}
