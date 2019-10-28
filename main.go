package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"

	log "github.com/sirupsen/logrus"

	"lproxyc/server"
)

var (
	listenAddr = ""
	wsPath     = ""
	daemon     = ""

	uuid      = ""
	url       = ""
	tunnelCap = 2
	reqCap    = 200
)

func init() {
	flag.StringVar(&listenAddr, "l", "127.0.0.1:8020", "specify the listen address")
	flag.StringVar(&daemon, "d", "yes", "specify daemon mode")
	flag.StringVar(&url, "url", "", "specify the url")
	flag.StringVar(&uuid, "uuid", "", "specify uuid")
	flag.IntVar(&tunnelCap, "tunc", 2, "specify tunnel capacity")
	flag.IntVar(&reqCap, "reqc", 200, "specify request capacity")
}

// getVersion get version
func getVersion() string {
	return "0.1.0"
}

func main() {
	// only one thread
	runtime.GOMAXPROCS(1)

	version := flag.Bool("v", false, "show version")

	flag.Parse()

	if *version {
		fmt.Printf("%s\n", getVersion())
		os.Exit(0)
	}

	if url == "" {
		fmt.Println("need url")
		os.Exit(1)
	}

	if uuid == "" {
		fmt.Println("need uuid")
		os.Exit(1)
	}

	log.Printf("uuid:%s, url:%s", uuid, url)
	log.Println("try to start  linproxy-c server, version:", getVersion())

	// start http server
	go server.CreateSocks5erver(listenAddr, url, uuid, tunnelCap, reqCap)
	log.Println("start linproxy-c server ok!")

	if daemon == "yes" {
		waitForSignal()
	} else {
		waitInput()
	}
	return
}

func waitInput() {
	var cmd string
	for {
		_, err := fmt.Scanf("%s\n", &cmd)
		if err != nil {
			//log.Println("Scanf err:", err)
			continue
		}

		switch cmd {
		case "exit", "quit":
			log.Println("exit by user")
			return
		case "gr":
			log.Println("current goroutine count:", runtime.NumGoroutine())
			break
		case "gd":
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			break
		default:
			break
		}
	}
}

func dumpGoRoutinesInfo() {
	log.Println("current goroutine count:", runtime.NumGoroutine())
	// use DEBUG=2, to dump stack like golang dying due to an unrecovered panic.
	pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
}
