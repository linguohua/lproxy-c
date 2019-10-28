package server

import (
	log "github.com/sirupsen/logrus"

	"lproxyc/socks5"
)

var (
	account Account
)

// CreateSocks5erver start http server
func CreateSocks5erver(listenAddr string, url string, uuid string, tunCap int, reqCap int) {
	account := newAccount(reqCap, uuid, url, tunCap)
	account.buildTunnels()

	config := &socks5.Config{ReqHandler: account}
	s, err := socks5.New(config)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("socks server listen at:%s", listenAddr)
	log.Fatal(s.ListenAndServe("tcp", listenAddr))
}
