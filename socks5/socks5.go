package socks5

// credit: https://github.com/armon/go-socks5
// credit: https://github.com/armon/go-socks5/blob/master/socks5.go
import (
	"bufio"
	"fmt"
	"net"

	log "github.com/sirupsen/logrus"
)

const (
	socks5Version = uint8(5)
)

// RequestHandler request handler
type RequestHandler interface {
	HandleRequest(req *SocksRequest) error
}

// Config is used to setup and configure a Server
type Config struct {
	// AuthMethods can be provided to implement custom authentication
	// By default, "auth-less" mode is enabled.
	// For password-based auth use UserPassAuthenticator.
	authMethods []authenticator

	// If provided, username/password authentication is enabled,
	// by appending a UserPassAuthenticator to AuthMethods. If not provided,
	// and AUthMethods is nil, then "auth-less" mode is enabled.
	credentials CredentialStore

	ReqHandler RequestHandler
}

// Server is reponsible for accepting connections and handling
// the details of the SOCKS5 protocol
type Server struct {
	config      *Config
	authMethods map[uint8]authenticator
}

// New creates a new Server and potentially returns an error
func New(conf *Config) (*Server, error) {
	// Ensure we have at least one authentication method enabled
	if len(conf.authMethods) == 0 {
		if conf.credentials != nil {
			conf.authMethods = []authenticator{&UserPassAuthenticator{conf.credentials}}
		} else {
			conf.authMethods = []authenticator{&NoAuthAuthenticator{}}
		}
	}

	server := &Server{
		config: conf,
	}

	server.authMethods = make(map[uint8]authenticator)

	for _, a := range conf.authMethods {
		server.authMethods[a.getCode()] = a
	}

	return server, nil
}

// ListenAndServe is used to create a listener and serve on it
func (s *Server) ListenAndServe(network, addr string) error {
	l, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	return s.Serve(l)
}

// Serve is used to serve connections from a listener
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go func() {
			err2 := s.ServeConn(conn)
			if err2 != nil {
				log.Println("s.ServeConn failed:", err2)
			}
		}()
	}
}

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) error {
	// log.Println("socks5 ServeConn")

	defer conn.Close()
	bufConn := bufio.NewReader(conn)

	// Read the version byte
	version := []byte{0}
	if _, err := bufConn.Read(version); err != nil {
		log.Printf("[ERR] socks: Failed to get version byte: %v", err)
		return err
	}

	// Ensure we are compatible
	if version[0] != socks5Version {
		err := fmt.Errorf("Unsupported SOCKS version: %v", version)
		log.Printf("[ERR] socks: %v", err)
		return err
	}

	// Authenticate the connection
	authContext, err := s.authenticate(conn, bufConn)
	if err != nil {
		err = fmt.Errorf("Failed to authenticate: %v", err)
		log.Printf("[ERR] socks: %v", err)
		return err
	}

	request, err := NewRequest(bufConn, conn)
	if err != nil {
		if err == errUnrecognizedAddrType {
			if err := sendReply(conn, addrTypeNotSupported, nil); err != nil {
				return fmt.Errorf("Failed to send reply: %v", err)
			}
		}
		return fmt.Errorf("Failed to read destination address: %v", err)
	}
	request.AuthContext = authContext

	// Process the client request
	if err := s.handleRequest(request, conn); err != nil {
		err = fmt.Errorf("Failed to handle request: %v", err)
		log.Printf("[ERR] socks: %v", err)
		return err
	}

	err = s.config.ReqHandler.HandleRequest(request)
	if err != nil {
		return fmt.Errorf("Failed to HandleRequest: %v", err)
	}

	return nil
}
